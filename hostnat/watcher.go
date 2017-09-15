package hostnat

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/utils"
)

var (
	reapplyEvery        = 5 * time.Minute
	disableHostNatIPset = "RANCHER_DISABLE_HOST_NAT_IPSET"
)

// Watch is used to look for changes in metadata and apply hostnat related rules
func Watch(c metadata.Client) error {
	w := &watcher{
		c:       c,
		applied: map[string]MASQRule{},
	}
	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]MASQRule
	lastApplied time.Time
}

// MASQRule is used to store the needed information for building
// a masquerading rule
type MASQRule struct {
	Subnet string
	Bridge string
}

func (p MASQRule) iptables() []byte {
	_, err := exec.Command("/sbin/ipset", "create", "--exist", disableHostNatIPset, "hash:net").CombinedOutput()
	if err != nil {
		logrus.Errorf("Failed to create ipset: %v", err)
	}
	buf := &bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("-A CATTLE_NAT_POSTROUTING -d %s -s %s -j ACCEPT\n", os.Getenv("METADATA_IP"), p.Subnet))
	buf.WriteString(fmt.Sprintf("-A CATTLE_NAT_POSTROUTING -p tcp -s %s -m set ! --match-set %s dst ! -o %s -j MASQUERADE --to-ports 1024-65535\n", p.Subnet, disableHostNatIPset, p.Bridge))
	buf.WriteString(fmt.Sprintf("-A CATTLE_NAT_POSTROUTING -p udp -s %s -m set ! --match-set %s dst ! -o %s -j MASQUERADE --to-ports 1024-65535\n", p.Subnet, disableHostNatIPset, p.Bridge))
	buf.WriteString(fmt.Sprintf("-A CATTLE_NAT_POSTROUTING -s %s -m set ! --match-set %s dst ! -o %s -j MASQUERADE\n", p.Subnet, disableHostNatIPset, p.Bridge))

	// LOCAL src
	buf.WriteString(fmt.Sprintf("-A CATTLE_NAT_POSTROUTING -o %s -m addrtype --src-type LOCAL --dst-type UNICAST -j MASQUERADE", p.Bridge))
	return buf.Bytes()
}

func (p MASQRule) localRoutingSetting() string {
	s := ""
	if p.Bridge != "" {
		s = fmt.Sprintf("net.ipv4.conf.%v.route_localnet=1", p.Bridge)
	}

	return s
}

func (w *watcher) run(args ...string) error {
	logrus.Debugf("Running %s", strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (w *watcher) onChangeNoError(version string) {
	if err := w.onChange(version); err != nil {
		logrus.Errorf("Failed to apply host rules: %v", err)
	}
}

func (w *watcher) onChange(version string) error {
	logrus.Debug("Evaluating NAT host rules")
	newRules := map[string]MASQRule{}

	networks, err := w.c.GetNetworks()
	if err != nil {
		return err
	}

	host, err := w.c.GetSelfHost()
	if err != nil {
		return err
	}

	for _, network := range networks {
		rule := w.networkToRule(network, host)
		if rule != nil {
			newRules[network.UUID] = *rule
		}
	}

	logrus.Debugf("New generated nat rules: %v", newRules)
	if !reflect.DeepEqual(w.applied, newRules) {
		logrus.Infof("Applying new nat rules")
		return w.apply(newRules)
	} else if time.Now().Sub(w.lastApplied) > reapplyEvery {
		return w.apply(newRules)
	}

	logrus.Debugf("No change in applied nat rules")
	return nil
}

func (w *watcher) networkToRule(network metadata.Network, host metadata.Host) *MASQRule {
	conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	for _, file := range conf {
		file = utils.UpdateCNIConfigByKeywords(file, host)
		props, _ := file.(map[string]interface{})
		hostNat, _ := props["hostNat"].(bool)
		cniType, _ := props["type"].(string)
		bridge, _ := props["bridge"].(string)
		bridgeSubnet, _ := props["bridgeSubnet"].(string)

		if hostNat && cniType == "rancher-bridge" && bridge != "" && bridgeSubnet != "" {
			return &MASQRule{
				Subnet: bridgeSubnet,
				Bridge: bridge,
			}
		}
	}

	return nil
}

func (w *watcher) enableLocalNetRouting(rules map[string]MASQRule) error {
	for _, rule := range rules {
		s := rule.localRoutingSetting()
		if s != "" {
			logrus.Debugf("s: %v", s)
			cmd := exec.Command("sysctl", "-w", s)
			var outBuf bytes.Buffer
			cmd.Stdout = &outBuf
			if err := cmd.Run(); err != nil {
				logrus.Errorf("error enabling local net routing: %v", err)
				return err
			}
			logrus.Debugf("Running %s, output: %s", s, outBuf.String())
		}
	}

	return nil
}

func (w *watcher) apply(rules map[string]MASQRule) error {
	if err := w.enableLocalNetRouting(rules); err != nil {
		return err
	}

	buf := &bytes.Buffer{}
	buf.WriteString("*nat\n")
	buf.WriteString(":CATTLE_NAT_POSTROUTING -\n")
	buf.WriteString("-F CATTLE_NAT_POSTROUTING\n")
	for _, rule := range rules {
		buf.WriteString("\n")
		buf.Write(rule.iptables())
	}

	buf.WriteString("\nCOMMIT\n")

	if logrus.GetLevel() == logrus.DebugLevel {
		fmt.Printf("Applying rules\n%s", buf)
	}

	cmd := exec.Command("iptables-restore", "-n")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = buf
	if err := cmd.Run(); err != nil {
		logrus.Errorf("Failed to apply rules\n%s", buf)
		return err
	}

	w.applied = rules
	w.lastApplied = time.Now()
	return nil
}
