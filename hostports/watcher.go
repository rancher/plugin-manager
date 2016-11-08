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
)

var (
	reapplyEvery = 5 * time.Minute
)

func Watch(c metadata.Client) error {
	w := &watcher{
		c:       c,
		applied: map[string]PortRule{},
	}

	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]PortRule
	lastApplied time.Time
}

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
	buf.WriteString("-A CATTLE_PREROUTING")
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

func (p PortRule) iptables() []byte {
	// Rules like
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j MARK --set-mark 4200
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j DNAT --to ${targetIP}:${targetPort}
	// We use mark 4200.  It is important whatever mark we use that the 0x8000 and 0x4000 bits are unset.
	// Those bits are used by k8s and will conflict.
	buf := &bytes.Buffer{}
	buf.Write(p.prefix())
	buf.WriteString(" -j MARK --set-mark 4200\n")

	buf.Write(p.prefix())
	buf.WriteString(" -j DNAT --to ")
	buf.WriteString(p.TargetIP)
	buf.WriteString(":")
	buf.WriteString(p.TargetPort)

	return buf.Bytes()
}

func (w *watcher) insertBaseRules() error {
	if w.run("iptables", "-t", "nat", "-C", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_PREROUTING") != nil {
		return w.run("iptables", "-t", "nat", "-I", "PREROUTING", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "CATTLE_PREROUTING")
	}
	if w.run("iptables", "-C", "FORWARD", "-j", "CATTLE_FORWARD") != nil {
		return w.run("iptables", "-I", "FORWARD", "-j", "CATTLE_FORWARD")
	}
	return nil
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

		if container.HostUUID != host.UUID ||
			!network.HostPorts ||
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
	// NOTE: We don't use CATTLE_POSTROUTING, but for migration we just wipe it out
	buf.WriteString("*nat\n:CATTLE_PREROUTING -\n:CATTLE_POSTROUTING -\n")
	buf.WriteString("-F CATTLE_PREROUTING\n-F CATTLE_POSTROUTING\n")
	for _, rule := range rules {
		buf.WriteString("\n")
		buf.Write(rule.iptables())
	}

	buf.WriteString("\nCOMMIT\n\n*filter\n:CATTLE_FORWARD -\n")
	buf.WriteString("-F CATTLE_FORWARD\n")
	buf.WriteString("-A CATTLE_FORWARD -m mark --mark 4200 -j ACCEPT\n")

	buf.WriteString("\nCOMMIT\n")

	if logrus.GetLevel() == logrus.DebugLevel {
		fmt.Printf("Applying rules\n%s", buf)
	}

	cmd := exec.Command("iptables-restore", "-n")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = buf
	if err := cmd.Run(); err != nil {
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
