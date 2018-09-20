package conntrack

import (
	"fmt"
	"net"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/ti-mo/netfilter"
)

const (
	opUnTup   = "Tuple unmarshal"
	opUnIPTup = "IPTuple unmarshal"
	opUnPTup  = "ProtoTuple unmarshal"
)

// A Tuple holds an IPTuple, ProtoTuple and a Zone.
type Tuple struct {
	IP    IPTuple
	Proto ProtoTuple
	Zone  uint16
}

// Filled returns true if the Tuple's IP and Proto members are filled.
// The Zone attribute is not considered, because it is zero in most cases.
func (t Tuple) Filled() bool {
	return t.IP.Filled() && t.Proto.Filled()
}

// String returns a string representation of a Tuple.
func (t Tuple) String() string {
	return fmt.Sprintf("<%s, Src: %s, Dst: %s>",
		ProtoLookup(t.Proto.Protocol),
		net.JoinHostPort(t.IP.SourceAddress.String(), strconv.Itoa(int(t.Proto.SourcePort))),
		net.JoinHostPort(t.IP.DestinationAddress.String(), strconv.Itoa(int(t.Proto.DestinationPort))),
	)
}

// UnmarshalAttribute unmarshals a netfilter.Attribute into a Tuple.
func (t *Tuple) UnmarshalAttribute(attr netfilter.Attribute) error {

	if !attr.Nested {
		return errors.Wrap(errNotNested, opUnTup)
	}

	if len(attr.Children) < 2 {
		return errors.Wrap(errNeedChildren, opUnTup)
	}

	for _, iattr := range attr.Children {
		switch TupleType(iattr.Type) {
		case CTATupleIP:
			var ti IPTuple
			if err := (&ti).UnmarshalAttribute(iattr); err != nil {
				return err
			}
			t.IP = ti
		case CTATupleProto:
			var tp ProtoTuple
			if err := (&tp).UnmarshalAttribute(iattr); err != nil {
				return err
			}
			t.Proto = tp
		case CTATupleZone:
			if len(iattr.Data) != 2 {
				return errIncorrectSize
			}
			t.Zone = iattr.Uint16()
		default:
			return errors.Wrap(fmt.Errorf(errAttributeChild, iattr.Type, AttributeType(attr.Type)), opUnTup)
		}
	}

	return nil
}

// MarshalAttribute marshals a Tuple to a netfilter.Attribute.
func (t Tuple) MarshalAttribute(at AttributeType) (netfilter.Attribute, error) {

	nfa := netfilter.Attribute{Type: uint16(at), Nested: true, Children: make([]netfilter.Attribute, 2, 3)}

	ipt, err := t.IP.MarshalAttribute()
	if err != nil {
		return netfilter.Attribute{}, err
	}
	nfa.Children[0] = ipt

	nfa.Children[1] = t.Proto.MarshalAttribute()

	if t.Zone != 0 {
		nfa.Children = append(nfa.Children, netfilter.Attribute{Type: uint16(CTATupleZone), Data: netfilter.Uint16Bytes(t.Zone)})
	}

	return nfa, nil

}

// An IPTuple encodes a source and destination address.
// Both of its members are of type net.IP.
type IPTuple struct {
	SourceAddress      net.IP
	DestinationAddress net.IP
}

// Filled returns true if the IPTuple's fields are non-zero.
func (ipt IPTuple) Filled() bool {
	return len(ipt.SourceAddress) != 0 && len(ipt.DestinationAddress) != 0
}

// UnmarshalAttribute unmarshals a netfilter.Attribute into an IPTuple.
// IPv4 addresses will be represented by a 4-byte net.IP, IPv6 addresses by 16-byte.
// The net.IP object is created with the raw bytes, NOT with net.ParseIP().
// Use IP.Equal() to compare addresses in implementations and tests.
func (ipt *IPTuple) UnmarshalAttribute(attr netfilter.Attribute) error {

	if TupleType(attr.Type) != CTATupleIP {
		return fmt.Errorf(errAttributeWrongType, attr.Type, CTATupleIP)
	}

	if !attr.Nested {
		return errors.Wrap(errNotNested, opUnIPTup)
	}

	if len(attr.Children) != 2 {
		return errors.Wrap(errNeedChildren, opUnIPTup)
	}

	for _, iattr := range attr.Children {

		if len(iattr.Data) != 4 && len(iattr.Data) != 16 {
			return errIncorrectSize
		}

		switch IPTupleType(iattr.Type) {
		case CTAIPv4Src, CTAIPv6Src:
			ipt.SourceAddress = net.IP(iattr.Data)
		case CTAIPv4Dst, CTAIPv6Dst:
			ipt.DestinationAddress = net.IP(iattr.Data)
		default:
			return errors.Wrap(fmt.Errorf(errAttributeChild, iattr.Type, CTATupleIP), opUnIPTup)
		}
	}

	return nil
}

// MarshalAttribute marshals an IPTuple to a netfilter.Attribute.
func (ipt IPTuple) MarshalAttribute() (netfilter.Attribute, error) {

	// If either address is not a valid IP or if they do not belong to the same address family, returns false.
	// Taken from net.IP, for some reason this function is not exported.
	matchAddrFamily := func(ip net.IP, x net.IP) bool {
		return ip.To4() != nil && x.To4() != nil || ip.To16() != nil && ip.To4() == nil && x.To16() != nil && x.To4() == nil
	}

	// Ensure that source and destination belong to the same address family.
	if !matchAddrFamily(ipt.SourceAddress, ipt.DestinationAddress) {
		return netfilter.Attribute{}, errBadIPTuple
	}

	nfa := netfilter.Attribute{Type: uint16(CTATupleIP), Nested: true, Children: make([]netfilter.Attribute, 2)}

	// To4() returns nil if the IP is not a 4-byte array nor a 16-byte array with markers
	// To4() will always return a 4-byte array. To16() will always return a 16-byte array, potentially with markers.
	// In the case below, To16 can never return markers, because the 4-byte case is caught by To4().
	if src, dest := ipt.SourceAddress.To4(), ipt.DestinationAddress.To4(); src != nil && dest != nil {
		nfa.Children[0] = netfilter.Attribute{Type: uint16(CTAIPv4Src), Data: src}
		nfa.Children[1] = netfilter.Attribute{Type: uint16(CTAIPv4Dst), Data: dest}
	} else {
		// Here, we know that both addresses are of same size and not 4 bytes long, assume 16.
		nfa.Children[0] = netfilter.Attribute{Type: uint16(CTAIPv6Src), Data: ipt.SourceAddress.To16()}
		nfa.Children[1] = netfilter.Attribute{Type: uint16(CTAIPv6Dst), Data: ipt.DestinationAddress.To16()}
	}

	return nfa, nil
}

// IsIPv6 returns true if the IPTuple contains source and destination addresses that are both IPv6.
func (ipt IPTuple) IsIPv6() bool {
	return ipt.SourceAddress.To16() != nil && ipt.SourceAddress.To4() == nil &&
		ipt.DestinationAddress.To16() != nil && ipt.DestinationAddress.To4() == nil
}

// A ProtoTuple encodes a protocol number, source port and destination port.
type ProtoTuple struct {
	Protocol        uint8
	SourcePort      uint16
	DestinationPort uint16

	ICMPv4 bool
	ICMPv6 bool

	ICMPID   uint16
	ICMPType uint8
	ICMPCode uint8
}

// Filled returns true if the ProtoTuple's protocol is non-zero.
func (pt ProtoTuple) Filled() bool {
	return pt.Protocol != 0
}

// UnmarshalAttribute unmarshals a netfilter.Attribute into a ProtoTuple.
func (pt *ProtoTuple) UnmarshalAttribute(attr netfilter.Attribute) error {

	if TupleType(attr.Type) != CTATupleProto {
		return fmt.Errorf(errAttributeWrongType, attr.Type, CTATupleProto)
	}

	if !attr.Nested {
		return errors.Wrap(errNotNested, opUnPTup)
	}

	if len(attr.Children) == 0 {
		return errors.Wrap(errNeedSingleChild, opUnPTup)
	}

	for _, iattr := range attr.Children {
		switch ProtoTupleType(iattr.Type) {
		case CTAProtoNum:
			pt.Protocol = iattr.Data[0]

			if pt.Protocol == syscall.IPPROTO_ICMP {
				pt.ICMPv4 = true
			} else if pt.Protocol == syscall.IPPROTO_ICMPV6 {
				pt.ICMPv6 = true
			}
		case CTAProtoSrcPort:
			pt.SourcePort = iattr.Uint16()
		case CTAProtoDstPort:
			pt.DestinationPort = iattr.Uint16()
		case CTAProtoICMPID, CTAProtoICMPv6ID:
			pt.ICMPID = iattr.Uint16()
		case CTAProtoICMPType, CTAProtoICMPv6Type:
			pt.ICMPType = iattr.Data[0]
		case CTAProtoICMPCode, CTAProtoICMPv6Code:
			pt.ICMPCode = iattr.Data[0]
		default:
			return errors.Wrap(fmt.Errorf(errAttributeChild, iattr.Type, CTATupleProto), opUnPTup)
		}
	}

	return nil
}

// MarshalAttribute marshals a ProtoTuple into a netfilter.Attribute.
func (pt ProtoTuple) MarshalAttribute() netfilter.Attribute {

	nfa := netfilter.Attribute{Type: uint16(CTATupleProto), Nested: true, Children: make([]netfilter.Attribute, 3, 4)}

	nfa.Children[0] = netfilter.Attribute{Type: uint16(CTAProtoNum), Data: []byte{pt.Protocol}}

	switch pt.Protocol {
	case unix.IPPROTO_ICMP:
		nfa.Children[1] = netfilter.Attribute{Type: uint16(CTAProtoICMPType), Data: []byte{pt.ICMPType}}
		nfa.Children[2] = netfilter.Attribute{Type: uint16(CTAProtoICMPCode), Data: []byte{pt.ICMPCode}}
		nfa.Children = append(nfa.Children, netfilter.Attribute{Type: uint16(CTAProtoICMPID), Data: netfilter.Uint16Bytes(pt.ICMPID)})
	case unix.IPPROTO_ICMPV6:
		nfa.Children[1] = netfilter.Attribute{Type: uint16(CTAProtoICMPv6Type), Data: []byte{pt.ICMPType}}
		nfa.Children[2] = netfilter.Attribute{Type: uint16(CTAProtoICMPv6Code), Data: []byte{pt.ICMPCode}}
		nfa.Children = append(nfa.Children, netfilter.Attribute{Type: uint16(CTAProtoICMPv6ID), Data: netfilter.Uint16Bytes(pt.ICMPID)})
	default:
		nfa.Children[1] = netfilter.Attribute{Type: uint16(CTAProtoSrcPort), Data: netfilter.Uint16Bytes(pt.SourcePort)}
		nfa.Children[2] = netfilter.Attribute{Type: uint16(CTAProtoDstPort), Data: netfilter.Uint16Bytes(pt.DestinationPort)}
	}

	return nfa
}
