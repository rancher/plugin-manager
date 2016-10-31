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
	RancherSystemLabelKey = "io.rancher.container.system"
	RancherNameserver     = "169.254.169.250"
	RancherDomain         = "rancher.internal"
	RancherDNS            = "io.rancher.container.dns"
	CNILabel              = "io.rancher.cni.network"
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
	return defaultDomains
}

func setupResolvConf(container *docker.Container) error {
	if _, ok := container.Config.Labels[RancherSystemLabelKey]; ok {
		return nil
	}

	p := container.ResolvConfPath
	input, err := os.Open(p)
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
			for _, domain := range getDNSSearch(container) {
				if strings.Contains(text, " "+domain) {
					continue
				}
				text = text + " " + domain
			}
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
	return ioutil.WriteFile(p, buffer.Bytes(), 0666)
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

	if c.Config.Labels[CNILabel] != "" || c.Config.Labels[RancherDNS] == "true" {
		log.Infof("Setting up resolv.conf for ContainerId [%s]", event.ID)
		return setupResolvConf(c)
	}

	return nil
}
