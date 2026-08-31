package discovery

import (
	"net"
	"testing"
)

func TestSelfFilterAndAddressUpdate(t *testing.T) {
	s := &Service{selfID: "self", peers: map[string]Peer{}}
	s.ObserveForTest(Peer{ID: "self"})
	if len(s.Peers()) != 0 {
		t.Fatal("kept self")
	}
	s.ObserveForTest(Peer{ID: "x", Addresses: []string{"1:2"}})
	s.ObserveForTest(Peer{ID: "x", Addresses: []string{"3:4"}})
	p, _ := s.Find("x")
	if p.Addresses[0] != "3:4" {
		t.Fatal(p)
	}
}

func TestEligibleLANInterface(t *testing.T) {
	if !eligibleInterface(net.Interface{Name: "en0", Flags: net.FlagUp | net.FlagMulticast}) {
		t.Fatal("rejected LAN interface")
	}
	for _, name := range []string{"tailscale0", "docker0", "bridge100", "vmnet8", "wg0"} {
		if eligibleInterface(net.Interface{Name: name, Flags: net.FlagUp | net.FlagMulticast}) {
			t.Fatalf("accepted %s", name)
		}
	}
	if eligibleInterface(net.Interface{Name: "lo", Flags: net.FlagUp | net.FlagMulticast | net.FlagLoopback}) {
		t.Fatal("accepted loopback")
	}
}

func TestIPInDirectLANNetworks(t *testing.T) {
	_, v4, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	_, v6, err := net.ParseCIDR("fd00:1234::/64")
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.0.2.20", "fd00:1234::20"} {
		if !ipInNetworks(net.ParseIP(ip), []*net.IPNet{v4, v6}) {
			t.Fatalf("rejected direct LAN address %s", ip)
		}
	}
	for _, ip := range []string{"198.51.100.20", "fd00:5678::20"} {
		if ipInNetworks(net.ParseIP(ip), []*net.IPNet{v4, v6}) {
			t.Fatalf("accepted routed address %s", ip)
		}
	}
}

func TestDirectEndpointRejectsRoutedAndLoopbackAddresses(t *testing.T) {
	_, network, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	addresses := []string{"127.0.0.1:24872", "198.51.100.20:24872", "192.0.2.20:24872"}
	got, ok := directEndpoint(addresses, []*net.IPNet{network})
	if !ok || got != "192.0.2.20:24872" {
		t.Fatalf("endpoint=%q ok=%v", got, ok)
	}
	if _, ok := directEndpoint(addresses[:2], []*net.IPNet{network}); ok {
		t.Fatal("accepted an address outside the direct LAN")
	}
}
