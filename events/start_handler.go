package events

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/rancher/event-subscriber/locks"
)

const (
	RancherNameserver  = "169.254.169.250"
	RancherDomain      = "rancher.internal"
	RancherDNS         = "io.rancher.container.dns"
	RancherDNSPriority = "io.rancher.container.dns.priority"
	RancherNetwork     = "io.rancher.container.network"
	CNILabel           = "io.rancher.cni.network"
)

type StartHandler struct {
	Client SimpleDockerClient
}

func getDNSSearch(container *docker.Container) []string {
	var defaultDomains []string
	var svcNameSpace string
	var stackNameSpace string

	//from labels - for upgraded systems
	if container.Config.Labels != nil {
		setRancherSearchDomains := true
		if strings.EqualFold(strings.TrimSpace(container.Config.Labels[RancherDNSPriority]), "None") {
			setRancherSearchDomains = false
		}
		if setRancherSearchDomains {
			if value, ok := container.Config.Labels["io.rancher.stack_service.name"]; ok {
				splitted := strings.Split(value, "/")
				svc := strings.ToLower(splitted[1])
				stack := strings.ToLower(splitted[0])
				svcNameSpace = svc + "." + stack + "." + RancherDomain
				stackNameSpace = stack + "." + RancherDomain
				defaultDomains = append(defaultDomains, svcNameSpace)
				defaultDomains = append(defaultDomains, stackNameSpace)
			}
		}
	}

	//from search domains
	if container.HostConfig.DNSSearch != nil {
		for _, domain := range container.HostConfig.DNSSearch {
			if domain != svcNameSpace && domain != stackNameSpace {
				defaultDomains = append(defaultDomains, domain)
			}
		}
	}

	// default rancher domain
	defaultDomains = append(defaultDomains, RancherDomain)

	log.Debugf("defaultDomains: %v", defaultDomains)
	return defaultDomains
}

func setupResolvConf(container *docker.Container) error {
	log.Debugf("setupResolvConf for container: %+v", container)
	if container.ResolvConfPath == "" {
		log.Debugf("container.ResolvConfPath is not set for container: %v", container.ID)
		return nil
	}

	if container.ResolvConfPath == "/etc/resolv.conf" {
		// Don't shoot ourself in the foot and change our own DNS
		log.Debugf("resolv.conf already set for container: %v, skipping", container.ID)
		return nil
	}

	input, err := os.Open(container.ResolvConfPath)
	if err != nil {
		return err
	}

	defer input.Close()

	var buffer bytes.Buffer
	scanner := bufio.NewScanner(input)
	searchSet := false
	nameserverSet := false
	for scanner.Scan() {
		text := scanner.Text()

		if strings.Contains(text, RancherNameserver) {
			nameserverSet = true
		} else if strings.HasPrefix(text, "nameserver") {
			text = "# " + text
		}

		if strings.HasPrefix(text, "search") {
			domainsToBeAdded := []string{}
			for _, domain := range getDNSSearch(container) {
				if strings.Contains(text, " "+domain) {
					continue
				}
				domainsToBeAdded = append(domainsToBeAdded, domain)
			}

			if container.Config.Labels[RancherDNSPriority] == "service_last" {
				text = text + " " + strings.Join(domainsToBeAdded, " ")
			} else {
				text = strings.Replace(text, "search", "search "+strings.Join(domainsToBeAdded, " "), 1)
			}
			log.Debugf("text: %v", text)
			searchSet = true
		}

		if _, err := buffer.Write([]byte(text)); err != nil {
			return err
		}

		if _, err := buffer.Write([]byte("\n")); err != nil {
			return err
		}
	}

	if !searchSet {
		buffer.Write([]byte("search " + strings.ToLower(strings.Join(getDNSSearch(container), " "))))
		buffer.Write([]byte("\n"))
	}

	if !nameserverSet {
		buffer.Write([]byte("nameserver "))
		buffer.Write([]byte(RancherNameserver))
		buffer.Write([]byte("\n"))
	}

	input.Close()
	return ioutil.WriteFile(container.ResolvConfPath, buffer.Bytes(), 0666)
}

func (h *StartHandler) Handle(event *docker.APIEvents) error {
	// Note: event.ID == container's ID
	lock := locks.Lock("start." + event.ID)
	if lock == nil {
		log.Debugf("Container locked. Can't run StartHandler. ID: [%s]", event.ID)
		return nil
	}
	defer lock.Unlock()

	c, err := h.Client.InspectContainer(event.ID)
	if err != nil {
		return err
	}

	if !c.State.Running {
		log.Infof("Container [%s] not running. Can't setup resolv.conf.", c.ID)
		return nil
	}

	if c.Config.Labels[RancherDNS] == "false" {
		return nil
	}

	if c.Config.Labels[CNILabel] != "" || c.Config.Labels[RancherDNS] == "true" ||
		c.Config.Labels[RancherNetwork] == "true" {
		log.Infof("Setting up resolv.conf for ContainerId [%s]", event.ID)
		return setupResolvConf(c)
	}

	return nil
}
