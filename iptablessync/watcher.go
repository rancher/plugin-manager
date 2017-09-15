// Package iptablessync is responsible for creating the necessary
// chains, ipsets, hooking them in the appropriate order and
// monitoring them
package iptablessync

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/utils"
)

var (
	// DefaultSyncInterval specifies the default value for arpsync interval in seconds
	DefaultSyncInterval = 120
)

// IPTablesWatcher makes sure the order of the chains is maintained
type IPTablesWatcher struct {
	syncInterval time.Duration
	mc           metadata.Client
}

type hookRule struct {
	table    string
	chain    string
	dstChain string
	spec     string
	num      string
}

// Watch checks the iptables chains periodically
func Watch(syncInterval int, mc metadata.Client) error {
	iptw := &IPTablesWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
	}

	if err := iptw.runFirstTime(); err != nil {
		logrus.Errorf("iptablessync: error running first time: %v", err)
		return err
	}

	go iptw.doSync()

	return nil
}

func (iptw *IPTablesWatcher) doSync() error {
	for {
		logrus.Debugf("iptablessync: sleeping for %v", iptw.syncInterval)
		time.Sleep(iptw.syncInterval)

		if err := iptw.checkAndHookChains(); err != nil {
			logrus.Errorf("iptablessync: error doing sync: %v", err)
		}
	}
}

func (iptw *IPTablesWatcher) runFirstTime() error {
	// Create the chains
	if err := iptw.createChains(); err != nil {
		logrus.Errorf("iptablessync: error creating chains: %v", err)
		return err
	}

	// check and hook chains
	if err := iptw.checkAndHookChains(); err != nil {
		logrus.Errorf("iptablessync: error while checkAndHookChains: %v", err)
		return err
	}

	return nil
}

func (iptw *IPTablesWatcher) createChains() error {
	buf := &bytes.Buffer{}

	buf.WriteString("*raw\n")
	buf.WriteString(":CATTLE_RAW_PREROUTING -\n")
	buf.WriteString("\nCOMMIT\n")

	buf.WriteString("*nat\n")
	buf.WriteString(":CATTLE_PREROUTING -\n")
	buf.WriteString(":CATTLE_NAT_POSTROUTING -\n")
	buf.WriteString(":CATTLE_HOSTPORTS_POSTROUTING -\n")
	buf.WriteString(":CATTLE_OUTPUT -\n")
	buf.WriteString("\nCOMMIT\n")

	buf.WriteString("*filter\n")
	buf.WriteString(":CATTLE_NETWORK_POLICY -\n")
	buf.WriteString(":CATTLE_FORWARD -\n")
	buf.WriteString("\nCOMMIT\n")

	if logrus.GetLevel() == logrus.DebugLevel {
		fmt.Printf("creating chains\n%s", buf)
	}

	cmd := exec.Command("iptables-restore", "-n")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = buf
	if err := cmd.Run(); err != nil {
		logrus.Errorf("iptablessync: failed to create chains\n%v", buf)
		return err
	}

	return nil
}

func checkOneHookRule(rule hookRule) error {
	var e error
	var cmd string

	install := false
	cmd = fmt.Sprintf("iptables -w -t %v -C %v %v", rule.table, rule.chain, rule.spec)
	if utils.RunNoStdoutNoStderr(cmd) != nil {
		logrus.Infof("iptablessync: need to hook %v chain", rule.dstChain)
		install = true
	} else {
		cmd = fmt.Sprintf("iptables -w -t %v -S %v %v", rule.table, rule.chain, rule.num)
		outputBytes, err := utils.RunOutput(cmd)
		output := string(outputBytes)
		if err != nil {
			logrus.Errorf("error running cmd: %v: %v", cmd, err)
			e = errors.Wrap(e, err.Error())
		} else {
			expected := fmt.Sprintf("-A %v %v\n", rule.chain, rule.spec)
			if output != expected {
				logrus.Debugf("iptablessync: expected: %v but output: %v", expected, output)
				logrus.Infof("iptablessync: fixing order for %v chain", rule.dstChain)
				cmd = fmt.Sprintf("iptables -w -t %v -D %v %v", rule.table, rule.chain, rule.spec)
				if err := utils.RunNoStdoutNoStderr(cmd); err != nil {
					e = errors.Wrap(e, err.Error())
				}
				install = true
			}
		}
	}
	if install {
		cmd = fmt.Sprintf("iptables -w -t %v -I %v %v %v", rule.table, rule.chain, rule.num, rule.spec)
		logrus.Infof("iptablessync: installing cmd: %v", cmd)
		if err := utils.RunNoStdoutNoStderr(cmd); err != nil {
			e = errors.Wrap(e, err.Error())
		}
	}

	return e
}

func (iptw *IPTablesWatcher) checkAndHookChains() error {
	var e error

	if err := checkOneHookRule(hookRule{
		table:    "raw",
		chain:    "PREROUTING",
		dstChain: "CATTLE_RAW_PREROUTING",
		spec:     "-m addrtype --dst-type LOCAL -j CATTLE_RAW_PREROUTING",
		num:      "1",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "nat",
		chain:    "PREROUTING",
		dstChain: "CATTLE_PREROUTING",
		spec:     "-m addrtype --dst-type LOCAL -j CATTLE_PREROUTING",
		num:      "1",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "nat",
		chain:    "OUTPUT",
		dstChain: "CATTLE_OUTPUT",
		spec:     "-m addrtype --dst-type LOCAL -j CATTLE_OUTPUT",
		num:      "1",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "nat",
		chain:    "POSTROUTING",
		dstChain: "CATTLE_NAT_POSTROUTING",
		spec:     "-j CATTLE_NAT_POSTROUTING",
		num:      "1",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "nat",
		chain:    "POSTROUTING",
		dstChain: "CATTLE_HOSTPORTS_POSTROUTING",
		spec:     "-j CATTLE_HOSTPORTS_POSTROUTING",
		num:      "2",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "filter",
		chain:    "FORWARD",
		dstChain: "CATTLE_NETWORK_POLICY",
		spec:     "-j CATTLE_NETWORK_POLICY",
		num:      "1",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	if err := checkOneHookRule(hookRule{
		table:    "filter",
		chain:    "FORWARD",
		dstChain: "CATTLE_FORWARD",
		spec:     "-j CATTLE_FORWARD",
		num:      "2",
	}); err != nil {
		e = errors.Wrap(e, err.Error())
	}

	return e
}
