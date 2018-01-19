package conntrack

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
	pmutils "github.com/rancher/plugin-manager/utils"
)

const (
	serviceClusterIPRangeLabel = "io.rancher.k8s.service.cluster.ip.range"
)

// CTEntry represents one entry from the conntrack table
// Plucking only interested fields, ignoring others
type CTEntry struct {
	Protocol                string
	OriginalSourceIP        string
	OriginalDestinationIP   string
	OriginalSourcePort      string
	OriginalDestinationPort string
	ReplySourceIP           string
	ReplyDestinationIP      string
	ReplySourcePort         string
	ReplyDestinationPort    string
}

// ListSNAT lists only SNAT conntrack entries
func ListSNAT() ([]CTEntry, error) {
	return cmdCTListSNAT()
}

// ListDNAT lists only DNAT conntrack entries
func ListDNAT() ([]CTEntry, error) {
	return cmdCTListDNAT()
}

// CTEntryCreate Addetes the given entry from the conntrack table
func CTEntryCreate(e CTEntry) error {
	cmd := exec.Command(
		"conntrack", "--create",
		"-p", e.Protocol,
		"--orig-src", e.OriginalSourceIP,
		"--orig-dst", e.OriginalDestinationIP,
		"--orig-port-src", e.OriginalSourcePort,
		"--orig-port-dst", e.OriginalDestinationPort,
		"--reply-src", e.ReplySourceIP,
		"--reply-dst", e.ReplyDestinationIP,
		"--reply-port-src", e.ReplySourcePort,
		"--reply-port-dst", e.ReplyDestinationPort,
		"--timeout", "120",
		"--state", "ESTABLISHED",
	)
	if err := cmd.Run(); err != nil {
		log.Errorf("error adding conntrack entry: %v", err)
		return err
	}
	return nil
}

// CTEntryDelete deletes the given entry from the conntrack table
func CTEntryDelete(e CTEntry) error {
	cmd := exec.Command(
		"conntrack", "--delete",
		"-p", e.Protocol,
		"--orig-src", e.OriginalSourceIP,
		"--orig-dst", e.OriginalDestinationIP,
		"--orig-port-dst", e.OriginalDestinationPort,
		"--reply-src", e.ReplySourceIP,
		"--reply-dst", e.ReplyDestinationIP,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("error deleting conntrack entry: %v", err)
		return err
	}
	return nil
}

func cmdCTListSNAT() ([]CTEntry, error) {
	out, err := exec.Command("conntrack", "-n", "-L").Output()
	if err != nil {
		log.Errorf("error getting SNAT conntrack entries")
		return nil, err
	}

	if len(out) == 0 {
		return nil, nil
	}
	return parseMultipleEntries(string(out)), nil
}

func cmdCTListDNAT() ([]CTEntry, error) {
	out, err := exec.Command("conntrack", "-g", "-L").Output()
	if err != nil {
		log.Errorf("error getting DNAT conntrack entries")
		return nil, err
	}

	if len(out) == 0 {
		return nil, nil
	}
	return parseMultipleEntries(string(out)), nil
}

func parseMultipleEntries(input string) []CTEntry {
	var entries []CTEntry
	for _, line := range strings.Split(input, "\n") {
		if line == "" {
			continue
		}
		e, err := parseOneConntrackEntry(line)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}

	return entries
}

func parseOneConntrackEntry(e string) (CTEntry, error) {
	log.Debugf("conntrack: parsing conntrack entry: %v", e)
	ctEntry := CTEntry{}

	original := make(map[string]string)
	reply := make(map[string]string)
	fields := strings.Fields(e)

	if len(fields) < 4 {
		return ctEntry, fmt.Errorf("conntrack: invalid entry")
	}

	protocol := fields[0]

	for _, field := range fields[3:] {
		if !(field == "[UNREPLIED]" || field == "[ASSURED]") {
			kv := strings.Split(field, "=")
			if len(kv) != 2 {
				continue
			}
			_, ok := original[kv[0]]

			var m map[string]string
			if ok {
				m = reply
			} else {
				m = original
			}

			m[kv[0]] = kv[1]
		}
	}

	ctEntry.Protocol = protocol
	ctEntry.OriginalSourceIP = original["src"]
	ctEntry.OriginalDestinationIP = original["dst"]
	ctEntry.OriginalSourcePort = original["sport"]
	ctEntry.OriginalDestinationPort = original["dport"]
	ctEntry.ReplySourceIP = reply["src"]
	ctEntry.ReplyDestinationIP = reply["dst"]
	ctEntry.ReplySourcePort = reply["sport"]
	ctEntry.ReplyDestinationPort = reply["dport"]

	return ctEntry, nil
}

func deleteEntries(entries []CTEntry) error {
	hasErrored := false
	for _, ctEntry := range entries {
		if err := CTEntryDelete(ctEntry); err != nil {
			log.Errorf("conntracksync: error deleting the conntrack entry: %v", err)
			hasErrored = true
		}
	}

	if hasErrored {
		return fmt.Errorf("error deleting conntrack entries")
	}

	return nil
}

func getMismatchDNATEntries(containersMap map[string]*metadata.Container, excludedSubnets []string) ([]CTEntry, error) {
	var mismatchEntries []CTEntry

	dCTEntries, err := ListDNAT()
	if err != nil {
		log.Errorf("conntracksync: error fetching DNAT conntrack entries")
		return mismatchEntries, err
	}

	for _, ctEntry := range dCTEntries {
		in, err := pmutils.IsIPInSubnets(ctEntry.OriginalDestinationIP, excludedSubnets)
		if err != nil {
			log.Errorf("conntracksync: error checking if ip in excluded subnets: %v", err)
		}
		if in {
			log.Debugf("conntracksync: ip=%v is in excludedSubnets, skipping", ctEntry.OriginalDestinationIP)
			continue
		}
		var c *metadata.Container
		var specificEntryFound, genericEntryFound bool
		specificKey := ctEntry.OriginalDestinationIP + ":" + ctEntry.OriginalDestinationPort + "/" + ctEntry.Protocol
		log.Debugf("getMismatchDNATEntries: specificKey=%v", specificKey)
		c, specificEntryFound = containersMap[specificKey]
		if !specificEntryFound {
			genericKey := "0.0.0.0:" + ctEntry.OriginalDestinationPort + "/" + ctEntry.Protocol
			log.Debugf("getMismatchDNATEntries: genericKey=%v", genericKey)
			c, genericEntryFound = containersMap[genericKey]
			if !genericEntryFound {
				continue
			}
		}
		log.Debugf("getMismatchDNATEntries: c=%+v", c)
		if c.PrimaryIp != "" && ctEntry.ReplySourceIP != c.PrimaryIp {
			log.Infof("conntracksync: found mismatching DNAT conntrack entry: %v. [expected: %v, got: %v]", ctEntry, c.PrimaryIp, ctEntry.ReplySourceIP)
			mismatchEntries = append(mismatchEntries, ctEntry)
		}
	}
	return mismatchEntries, nil
}

func SyncDNATEntries(containersMap map[string]*metadata.Container, excludedDNATSubnets []string) error {
	mismatchEntries, err := getMismatchDNATEntries(containersMap, excludedDNATSubnets)
	if err != nil {
		return err
	}
	err = deleteEntries(mismatchEntries)
	if err != nil {
		return err
	}
	return nil
}

func getMismatchSNATEntries(containersMap map[string]*metadata.Container) ([]CTEntry, error) {
	var mismatchEntries []CTEntry

	sCTEntries, err := ListSNAT()
	if err != nil {
		log.Errorf("conntracksync: error fetching SNAT conntrack entries")
		return mismatchEntries, err
	}

	for _, ctEntry := range sCTEntries {
		var c *metadata.Container
		var specificEntryFound, genericEntryFound bool
		specificKey := ctEntry.ReplyDestinationIP + ":" + ctEntry.ReplyDestinationPort + "/" + ctEntry.Protocol
		c, specificEntryFound = containersMap[specificKey]
		if !specificEntryFound {
			genericKey := "0.0.0.0:" + ctEntry.ReplyDestinationPort + "/" + ctEntry.Protocol
			c, genericEntryFound = containersMap[genericKey]
			if !genericEntryFound {
				continue
			}
		}
		if c.PrimaryIp != "" && ctEntry.OriginalSourceIP != c.PrimaryIp {
			log.Infof("conntracksync: found mismatching SNAT conntrack entry: %v. [expected: %v, got: %v]", ctEntry, c.PrimaryIp, ctEntry.OriginalSourceIP)
			mismatchEntries = append(mismatchEntries, ctEntry)
		}
	}
	return mismatchEntries, nil
}

func SyncSNATEntries(containersMap map[string]*metadata.Container) error {
	mismatchEntries, err := getMismatchSNATEntries(containersMap)
	if err != nil {
		return err
	}
	err = deleteEntries(mismatchEntries)
	if err != nil {
		return err
	}
	return nil
}

func SyncNATEntries(mc metadata.Client) error {
	containersMap, err := buildContainersMaps(mc)
	if err != nil {
		log.Errorf("conntracksync: error building containersMap")
		return err
	}

	excludedDNATSubnets, err := getExcludedSubnetsForDNAT(mc)
	if err != nil {
		return err
	}

	err = SyncDNATEntries(containersMap, excludedDNATSubnets)
	if err != nil {
		return err
	}

	err = SyncSNATEntries(containersMap)
	if err != nil {
		return err
	}

	return nil
}

func buildContainersMaps(mc metadata.Client) (map[string]*metadata.Container, error) {
	host, err := mc.GetSelfHost()
	if err != nil {
		log.Errorf("conntracksync: error fetching self host from metadata")
		return nil, err
	}

	containers, err := mc.GetContainers()
	if err != nil {
		log.Errorf("conntracksync: error fetching containers from metadata")
		return nil, err
	}
	containersMap := make(map[string]*metadata.Container)
	for index, aContainer := range containers {
		if !(aContainer.HostUUID == host.UUID &&
			pmutils.IsContainerConsideredRunning(aContainer) &&
			len(aContainer.Ports) > 0) {
			continue
		}

		for _, aPort := range aContainer.Ports {
			protocol := "tcp"
			splits := strings.Split(aPort, ":")
			if len(splits) != 3 {
				continue
			}
			hostIP := splits[0]
			hostPort := splits[1]
			targetPort := splits[2]
			parts := strings.Split(targetPort, "/")
			if len(parts) == 2 {
				protocol = parts[1]
			}

			containersMap[hostIP+":"+hostPort+"/"+protocol] = &containers[index]
		}
	}

	return containersMap, nil
}

func getExcludedSubnetsForDNAT(mc metadata.Client) ([]string, error) {
	var subnets []string
	services, err := mc.GetServices()
	if err != nil {
		return subnets, err
	}

	for _, aService := range services {
		if !(aService.Name == "kubernetes" && aService.Kind == "service") {
			continue
		}
		log.Debugf("aService: %v", aService)
		subnet := aService.Labels[serviceClusterIPRangeLabel]
		if subnet != "" {
			subnets = append(subnets, subnet)
		}
	}

	return subnets, nil
}
