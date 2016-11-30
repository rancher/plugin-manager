package network

import (
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func (s *state) hasNetwork(pid int) (bool, error) {
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return false, err
	}
	defer ns.Close()

	handler, err := netlink.NewHandleAt(ns)
	if err != nil {
		return false, err
	}
	defer handler.Delete()

	links, err := handler.LinkList()
	return len(links) > 1, err
}
