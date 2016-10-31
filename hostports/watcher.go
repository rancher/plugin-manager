package hostports

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
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
	if err := w.insertBaseRules(); err != nil {
		return err
	}

	go c.OnChange(5, w.onChangeNoError)
	go w.watchBaseRules()

	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]PortRule
	lastApplied time.Time
}

type PortRule struct {
	SourceIP   string
	SourcePort string
	TargetIP   string
	TargetPort string
	Protocol   string
}

func (p PortRule) prefix() []byte {
	buf := &bytes.Buffer{}
	buf.WriteString("-A CATTLE_PREROUTING -p ")
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
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j MARK --set-mark 420000
	// -A CATTLE_PREROUTING -p ${protocol} --dport ${sourcePort} -j DNAT --to ${targetIP}:${targetPort}
	buf := &bytes.Buffer{}
	buf.Write(p.prefix())
	buf.WriteString(" -j MARK --set-mark 420000\n")

	buf.Write(p.prefix())
	buf.WriteString(" -j DNAT --to ")
	buf.WriteString(p.TargetIP)
	buf.WriteString(":")
	buf.WriteString(p.TargetPort)

	return buf.Bytes()
}

func (w *watcher) insertBaseRules() error {
	if w.run("iptables", "-t", "nat", "-C", "PREROUTING", "-j", "CATTLE_PREROUTING") != nil {
		return w.run("iptables", "-t", "nat", "-I", "PREROUTING", "-j", "CATTLE_PREROUTING")
	}
	if w.run("iptables", "-C", "FORWARD", "-j", "CATTLE_FORWARD") != nil {
		return w.run("iptables", "-I", "FORWARD", "-j", "CATTLE_FORWARD")
	}
	return nil
}

func (w *watcher) watchBaseRules() {
	for {
		time.Sleep(time.Minute)
		if err := w.insertBaseRules(); err != nil {
			logrus.Errorf("Failed to install base rules: %v", err)
		}
	}
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
		if container.HostUUID != host.UUID ||
			!networks[container.NetworkUUID].HostPorts ||
			container.PrimaryIp == "" {
			continue
		}

		for _, port := range container.Ports {
			rule, ok := parsePortRule(host.AgentIP, container.PrimaryIp, port)
			if !ok {
				continue
			}

			newPortRules[container.ExternalId+"/"+port] = rule
		}
	}

	logrus.Debugf("New generated rules: %v", newPortRules)
	if !reflect.DeepEqual(w.applied, newPortRules) || time.Now().Sub(w.lastApplied) > reapplyEvery {
		logrus.Infof("Applying new port rules")
		return w.apply(newPortRules)
	} else {
		logrus.Debugf("No change in applied rules")
	}

	return nil
}

func (w *watcher) apply(rules map[string]PortRule) error {
	buf := &bytes.Buffer{}
	// NOTE: We don't use CATTLE_PREROUTING, but for migration we just wipe it out
	buf.WriteString("*nat\n:CATTLE_PREROUTING -\n:CATTLE_POSTROUTING -\n-F CATTLE_PREROUTING\n-F CATTLE_POSTROUTING\n")
	buf.WriteString(":CATTLE_FORWARD -\n-F CATTLE_FORWARD\n")
	buf.WriteString("-A CATTLE_FORWARD -m mark --mark 420000 -j ACCEPT\n")
	for _, rule := range rules {
		buf.WriteString("\n")
		buf.Write(rule.iptables())
	}

	buf.WriteString("\nCOMMIT\n")

	logrus.Debugf("Applying rules\n%s", buf)

	cmd := exec.Command("iptables-restore", "-n")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = buf
	if err := cmd.Run(); err != nil {
		return err
	}

	w.applied = rules
	w.lastApplied = time.Now()
	return nil
}

func parsePortRule(hostIP, targetIP, portDef string) (PortRule, bool) {
	proto := "tcp"
	parts := strings.Split(portDef, ":")
	if len(parts) != 3 {
		return PortRule{}, false
	}

	sourceIP, sourcePort, targetPort := parts[0], parts[1], parts[2]

	parts = strings.Split(parts[2], "/")
	if len(parts) == 2 {
		proto = parts[1]
	}

	return PortRule{
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
