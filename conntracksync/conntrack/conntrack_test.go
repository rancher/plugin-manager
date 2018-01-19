package conntrack

import (
	"testing"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// Some of the tests can run only when in development,
// remember to disable this before commiting the code.
const inDevelopment = false

func TestCmdListDNAT(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("debug")
	log.Debugf("TestCmdListDNAT")

	cmdCTListDNAT()
}

func TestParseOneConntrackEntry(t *testing.T) {
	log.SetLevelString("debug")
	log.Debugf("parsing testEntry1")
	testEntry1 := "tcp      6 65 TIME_WAIT src=172.22.101.1 dst=172.22.101.101 sport=59032 dport=9901 src=10.49.205.140 dst=172.22.101.1 sport=80 dport=59032 [ASSURED] mark=0 use=1"

	parseOneConntrackEntry(testEntry1)

	log.Debugf("parsing testEntry2")
	testEntry2 := "tcp      6 151 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=32985 dport=80 [UNREPLIED] src=169.254.169.250 dst=172.17.0.1 sport=80 dport=32985 mark=0 use=1"
	parseOneConntrackEntry(testEntry2)
}

func TestCTEntryCreateDelete(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("debug")
	var err error
	testEntry := "tcp      6 65 TIME_WAIT src=172.22.101.1 dst=172.22.101.101 sport=59032 dport=9901 src=10.49.205.140 dst=172.22.101.1 sport=80 dport=59032 [ASSURED] mark=0 use=1"

	e, _ := parseOneConntrackEntry(testEntry)

	err = CTEntryCreate(e)
	if err != nil {
		log.Errorf("error: %v", err)
		t.Fail()
	}

	err = CTEntryDelete(e)
	if err != nil {
		log.Errorf("error: %v", err)
		t.Fail()
	}
}

func TestListDNAT(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("debug")
	_, err := ListDNAT()
	if err != nil {
		log.Errorf("error getting DNAT entries: %v", err)
		t.Fail()
	}
}

func TestParseMultipleEntries(t *testing.T) {
	log.SetLevelString("debug")
	entries := `tcp      6 431998 ESTABLISHED src=172.22.101.102 dst=172.22.101.201 sport=51784 dport=8080 src=172.22.101.201 dst=172.22.101.102 sport=8080 dport=51784 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=10.49.59.122 dst=169.254.169.250 sport=46733 dport=80 src=169.254.169.250 dst=10.49.59.122 sport=80 dport=46733 [ASSURED] mark=0 use=1
udp      17 38 src=10.0.2.15 dst=10.0.2.3 sport=42683 dport=53 src=10.0.2.3 dst=10.0.2.15 sport=53 dport=42683 [ASSURED] mark=0 use=1
tcp      6 89 TIME_WAIT src=172.17.0.1 dst=169.254.169.250 sport=60829 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60829 [ASSURED] mark=0 use=1
tcp      6 84 TIME_WAIT src=172.17.0.1 dst=169.254.169.250 sport=60836 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60836 [ASSURED] mark=0 use=1
udp      17 25 src=172.22.101.1 dst=172.22.101.255 sport=17500 dport=17500 [UNREPLIED] src=172.22.101.255 dst=172.22.101.1 sport=17500 dport=17500 mark=0 use=1
udp      17 158 src=10.0.2.15 dst=10.0.2.3 sport=53607 dport=53 src=10.0.2.3 dst=10.0.2.15 sport=53 dport=53607 [ASSURED] mark=0 use=1
tcp      6 84 TIME_WAIT src=172.17.0.1 dst=169.254.169.250 sport=60837 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60837 [ASSURED] mark=0 use=1
udp      17 175 src=10.49.59.122 dst=172.22.101.101 sport=4500 dport=4500 src=172.22.101.101 dst=172.22.101.102 sport=4500 dport=4500 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=60840 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60840 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=60841 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60841 [ASSURED] mark=0 use=1
udp      17 176 src=10.0.2.15 dst=10.0.2.3 sport=34591 dport=53 src=10.0.2.3 dst=10.0.2.15 sport=53 dport=34591 [ASSURED] mark=0 use=1
udp      17 177 src=10.0.2.15 dst=10.0.2.3 sport=52958 dport=53 src=10.0.2.3 dst=10.0.2.15 sport=53 dport=52958 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.22.101.102 dst=172.22.101.201 sport=51785 dport=8080 src=172.22.101.201 dst=172.22.101.102 sport=8080 dport=51785 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=60839 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60839 [ASSURED] mark=0 use=1
tcp      6 117 TIME_WAIT src=10.0.2.15 dst=151.101.24.204 sport=52376 dport=80 src=151.101.24.204 dst=10.0.2.15 sport=80 dport=52376 [ASSURED] mark=0 use=1
tcp      6 432000 ESTABLISHED src=10.0.2.2 dst=10.0.2.15 sport=51851 dport=22 src=10.0.2.15 dst=10.0.2.2 sport=22 dport=51851 [ASSURED] mark=0 use=1
tcp      6 431996 ESTABLISHED src=172.17.0.2 dst=172.22.101.201 sport=59302 dport=8080 src=172.22.101.201 dst=172.22.101.102 sport=8080 dport=59302 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.2 dst=172.22.101.201 sport=32824 dport=8080 src=172.22.101.201 dst=172.22.101.102 sport=8080 dport=32824 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=60843 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60843 [ASSURED] mark=0 use=2
tcp      6 117 TIME_WAIT src=10.0.2.15 dst=130.89.148.14 sport=48550 dport=80 src=130.89.148.14 dst=10.0.2.15 sport=80 dport=48550 [ASSURED] mark=0 use=1
tcp      6 84 TIME_WAIT src=172.17.0.1 dst=169.254.169.250 sport=60835 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60835 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.22.101.102 dst=172.22.101.201 sport=51786 dport=8080 src=172.22.101.201 dst=172.22.101.102 sport=8080 dport=51786 [ASSURED] mark=0 use=1
tcp      6 151 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=32985 dport=80 [UNREPLIED] src=169.254.169.250 dst=172.17.0.1 sport=80 dport=32985 mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.1 dst=169.254.169.250 sport=60830 dport=80 src=169.254.169.250 dst=172.17.0.1 sport=80 dport=60830 [ASSURED] mark=0 use=1`

	parseMultipleEntries(entries)
}

func TestGetMismatchDNATEntries(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("debug")

	mc, err := metadata.NewClientAndWait("http://169.254.169.250/2016-07-29")
	if err != nil {
		log.Errorf("error creating metadata client")
		t.Fail()
	}
	containersMap, err := buildContainersMaps(mc)
	log.Debugf("containersMap: %+v", containersMap)
	if err != nil {
		log.Errorf("conntracksync: error building containersMap")
		t.Fail()
	}
	excludedDNATSubnets, err := getExcludedSubnetsForDNAT(mc)
	if err != nil {
		t.Fail()
	}

	mismatchEntries, err := getMismatchDNATEntries(containersMap, excludedDNATSubnets)
	if err != nil {
		log.Errorf("error fetching mismatch DNAT entries")
		t.Fail()
	}

	log.Debugf("mismatchEntries: %+v", mismatchEntries)
}
