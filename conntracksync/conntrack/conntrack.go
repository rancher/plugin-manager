package conntrack

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Sirupsen/logrus"
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
		logrus.Errorf("error adding conntrack entry: %v", err)
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
		logrus.Errorf("error deleting conntrack entry: %v", err)
		return err
	}
	return nil
}

func cmdCTListSNAT() ([]CTEntry, error) {
	out, err := exec.Command("conntrack", "-n", "-L").Output()
	if err != nil {
		logrus.Errorf("error getting SNAT conntrack entries")
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
		logrus.Errorf("error getting DNAT conntrack entries")
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
	logrus.Debugf("conntrack: parsing conntrack entry: %v", e)
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
