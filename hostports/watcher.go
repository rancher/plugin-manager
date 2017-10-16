package hostports

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/utils"
)

var (
	reapplyEvery              = 5 * time.Minute
	hostPortsLabel            = "io.rancher.network.host_ports"
	hostPortsPostRoutingChain = "CATTLE_HOSTPORTS_POSTROUTING"
)

// Watch is used to monitor metadata for changes
func Watch(c metadata.Client, metadataAddress, metadataListenPort string) error {
	w := &watcher{
		c:                  c,
		applied:            map[string]PortRule{},
		metadataAddress:    metadataAddress,
		metadataListenPort: metadataListenPort,
	}

	if err := setupKernelParameters(); err != nil {
		logrus.Errorf("error: %v", err)
	}

	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c                  metadata.Client
	applied            map[string]PortRule
	lastApplied        time.Time
	metadataAddress    string
	metadataListenPort string
}

// PortRule is used to store the needed information for building a
// iptables rule
type PortRule struct {
	Bridge     string
	SourceIP   string
	SourcePort string
	TargetIP   string
	TargetPort string
	Protocol   string
}

func (p PortRule) prefix() []byte {
	buf := &bytes.Buffer{}
	if p.Bridge != "" {
		buf.WriteString(" ! -i ")
		buf.WriteString(p.Bridge)
	}
	buf.WriteString(" -p ")
	buf.WriteString(p.Protocol)
	if p.SourceIP != "0.0.0.0" {
		buf.WriteString(" -d ")
		buf.WriteString(p.SourceIP)
	}
	buf.WriteString(" --dport ")
	buf.WriteString(p.SourcePort)
	return buf.Bytes()
}

func (p PortRule) rawIptables() []byte {
	// Rules like
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j MARK --set-mark 4200
	// We use mark 4200.  It is important whatever mark we use that the 0x8000 and 0x4000 bits are unset.
	// Those bits are used by k8s and will conflict.
	buf := &bytes.Buffer{}
	buf.WriteString("-A CATTLE_RAW_PREROUTING")
	buf.Write(p.prefix())
	buf.WriteString(" -j MARK --set-mark 4200\n")
	return buf.Bytes()
}

func (p PortRule) natIptables() []byte {
	// Rules like
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j DNAT --to ${targetIP}:${targetPort}
	buf := &bytes.Buffer{}
	buf.WriteString("-A CATTLE_PREROUTING")
	buf.Write(p.prefix())
	buf.WriteString(" -j DNAT --to ")
	buf.WriteString(p.TargetIP)
	buf.WriteString(":")
	buf.WriteString(p.TargetPort)

	if p.SourceIP == "0.0.0.0" {
		buf.WriteString(fmt.Sprintf("\n-A CATTLE_PREROUTING -p %v -m %v --dport %v -m addrtype --dst-type LOCAL -j DNAT --to-destination %v:%v",
			p.Protocol, p.Protocol, p.SourcePort, p.TargetIP, p.TargetPort))
	} else {
		buf.WriteString(fmt.Sprintf("\n-A CATTLE_PREROUTING -p %v -m %v --dport %v -d %v -j DNAT --to-destination %v:%v",
			p.Protocol, p.Protocol, p.SourcePort, p.SourceIP, p.TargetIP, p.TargetPort))
	}

	buf.WriteString(fmt.Sprintf("\n-A CATTLE_OUTPUT -p %v -m %v --dport %v -m addrtype --dst-type LOCAL -j DNAT --to-destination %v:%v",
		p.Protocol, p.Protocol, p.SourcePort, p.TargetIP, p.TargetPort))

	buf.WriteString(fmt.Sprintf("\n-A %s -s %v -d %v -p %v -m %v --dport %v -j MASQUERADE",
		hostPortsPostRoutingChain, p.TargetIP, p.TargetIP, p.Protocol, p.Protocol, p.TargetPort))

	return buf.Bytes()
}

func (w *watcher) insertBaseRules() error {
	var e error
	if w.run("iptables", "-w", "-t", "raw", "-C", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_RAW_PREROUTING") != nil {
		if err := w.run("iptables", "-w", "-t", "raw", "-I", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_RAW_PREROUTING"); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}
	if w.run("iptables", "-w", "-t", "nat", "-C", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_PREROUTING") != nil {
		if err := w.run("iptables", "-w", "-t", "nat", "-I", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_PREROUTING"); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}
	if w.run("iptables", "-w", "-C", "FORWARD", "-j", "CATTLE_FORWARD") != nil {
		if err := w.run("iptables", "-w", "-I", "FORWARD", "-j", "CATTLE_FORWARD"); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}
	if w.run("iptables", "-w", "-t", "nat", "-C", "OUTPUT", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_OUTPUT") != nil {
		if err := w.run("iptables", "-w", "-t", "nat", "-I", "OUTPUT", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_OUTPUT"); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}
	if w.run("iptables", "-w", "-t", "nat", "-C", "POSTROUTING", "-j", hostPortsPostRoutingChain) != nil {
		if err := w.run("iptables", "-w", "-t", "nat", "-I", "POSTROUTING", "-j", hostPortsPostRoutingChain); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}
	return e
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
	logrus.Debug("Creating rule set")
	newPortRules := map[string]PortRule{}

	host, err := w.c.GetSelfHost()
	if err != nil {
		return err
	}

	networks, err := networksByUUID(w.c)
	if err != nil {
		return err
	}

	containers, err := w.c.GetContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {
		network := networks[container.NetworkUUID]
		bridge := ""

		if !utils.IsContainerConsideredRunning(container) {
			continue
		}

		if container.HostUUID != host.UUID ||
			!(network.HostPorts || (container.System && container.Labels[hostPortsLabel] == "true")) ||
			container.PrimaryIp == "" {
			continue
		}

		conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
		for _, file := range conf {
			props, _ := file.(map[string]interface{})
			cniType, _ := props["type"].(string)
			checkBridge, _ := props["bridge"].(string)

			if cniType == "rancher-bridge" && checkBridge != "" {
				bridge = checkBridge
			}
		}

		for _, port := range container.Ports {
			rule, ok := parsePortRule(bridge, host.AgentIP, container.PrimaryIp, port)
			if !ok {
				continue
			}

			newPortRules[container.ExternalId+"/"+port] = rule
		}
	}

	logrus.Debugf("New generated rules: %v", newPortRules)
	if !reflect.DeepEqual(w.applied, newPortRules) {
		logrus.Infof("Applying new port rules")
		return w.apply(newPortRules)
	} else if time.Now().Sub(w.lastApplied) > reapplyEvery {
		return w.apply(newPortRules)
	}

	logrus.Debugf("No change in applied rules")
	return nil
}

func (w *watcher) apply(rules map[string]PortRule) error {
	buf := &bytes.Buffer{}
	buf.WriteString("*raw\n")
	buf.WriteString(":CATTLE_RAW_PREROUTING -\n")
	buf.WriteString("-F CATTLE_RAW_PREROUTING\n")
	for _, rule := range rules {
		buf.WriteString("\n")
		buf.Write(rule.rawIptables())
	}
	buf.WriteString("\nCOMMIT\n")

	// NOTE: We don't use CATTLE_POSTROUTING, but for migration we just wipe it out
	buf.WriteString("*nat\n")
	buf.WriteString(":CATTLE_PREROUTING -\n")
	buf.WriteString(":CATTLE_POSTROUTING -\n")
	buf.WriteString(":CATTLE_OUTPUT -\n")
	buf.WriteString(fmt.Sprintf(":%s -\n", hostPortsPostRoutingChain))
	buf.WriteString("-F CATTLE_PREROUTING\n")
	buf.WriteString("-F CATTLE_POSTROUTING\n")
	buf.WriteString("-F CATTLE_OUTPUT\n")
	buf.WriteString(fmt.Sprintf("-F %s\n", hostPortsPostRoutingChain))

	if w.metadataListenPort != "80" {
		buf.WriteString(fmt.Sprintf("-A CATTLE_PREROUTING -d %s/32 -p tcp -m tcp --dport 80 -j DNAT --to-destination 169.254.169.250:%s\n", w.metadataAddress, w.metadataListenPort))
		buf.WriteString(fmt.Sprintf("-A CATTLE_OUTPUT -d %s/32 -p tcp -m tcp --dport 80 -j DNAT --to-destination 169.254.169.250:%s\n", w.metadataAddress, w.metadataListenPort))
	}

	for _, rule := range rules {
		buf.WriteString("\n")
		buf.Write(rule.natIptables())
	}

	buf.WriteString("\nCOMMIT\n\n*filter\n:CATTLE_FORWARD -\n")
	buf.WriteString("-F CATTLE_FORWARD\n")
	buf.WriteString("-A CATTLE_FORWARD -m mark --mark 0x1068 -j ACCEPT\n")
	// For k8s
	buf.WriteString("-A CATTLE_FORWARD -m mark --mark 0x4000 -j ACCEPT\n")

	buf.WriteString("\nCOMMIT\n")

	if logrus.GetLevel() == logrus.DebugLevel {
		fmt.Printf("Applying rules\n%s", buf)
	}

	cmd := exec.Command("iptables-restore", "-n")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = buf
	if err := cmd.Run(); err != nil {
		logrus.Errorf("Failed to apply port rules\n%s", buf)
		return err
	}

	if err := w.insertBaseRules(); err != nil {
		return errors.Wrap(err, "Applying port base iptables rules")
	}

	w.applied = rules
	w.lastApplied = time.Now()
	return nil
}

func parsePortRule(bridge, hostIP, targetIP, portDef string) (PortRule, bool) {
	proto := "tcp"
	parts := strings.Split(portDef, ":")
	if len(parts) != 3 {
		return PortRule{}, false
	}

	sourceIP, sourcePort, targetPort := parts[0], parts[1], parts[2]

	parts = strings.Split(targetPort, "/")
	if len(parts) == 2 {
		targetPort = parts[0]
		proto = parts[1]
	}

	return PortRule{
		Bridge:     bridge,
		SourceIP:   sourceIP,
		SourcePort: sourcePort,
		TargetIP:   targetIP,
		TargetPort: targetPort,
		Protocol:   proto,
	}, true
}

func networksByUUID(c metadata.Client) (map[string]metadata.Network, error) {
	networkByUUID := map[string]metadata.Network{}
	networks, err := c.GetNetworks()
	if err != nil {
		return nil, err
	}

	for _, network := range networks {
		networkByUUID[network.UUID] = network
	}

	return networkByUUID, nil
}

func setupKernelParameters() error {
	s := "net.bridge.bridge-nf-call-iptables=1"
	cmd := exec.Command("sysctl", "-w", s)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	if err := cmd.Run(); err != nil {
		logrus.Errorf("error setting up kernel parameters: %v", err)
		return err
	}
	logrus.Debugf("Running %s, output: %s", s, outBuf.String())
	return nil
}
