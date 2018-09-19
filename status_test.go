package conntrack

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ti-mo/netfilter"
)

func TestStatusError(t *testing.T) {

	nfaNested := netfilter.Attribute{Type: uint16(CTAStatus), Nested: true}

	var s Status

	assert.EqualError(t, s.UnmarshalAttribute(nfaBadType), fmt.Sprintf(errAttributeWrongType, CTAUnspec, CTAStatus))
	assert.EqualError(t, s.UnmarshalAttribute(nfaNested), errors.Wrap(errNested, opUnStatus).Error())
}

func TestStatusMarshalTwoWay(t *testing.T) {

	tests := []struct {
		name   string
		b      []byte
		status Status
		err    error
	}{
		{
			name:   "default values",
			b:      []byte{0x00, 0x00, 0x00, 0x00},
			status: Status{},
		},
		{
			name:   "out of range, only highest bits flipped",
			b:      []byte{0xFF, 0xFF, 0x80, 0x00},
			status: Status{value: 0xFFFF8000},
		},
		{
			name: "error, byte array too short",
			b:    []byte{0xBE, 0xEF},
			err:  errors.Wrap(errIncorrectSize, opUnStatus),
		},
		{
			name: "error, byte array too long",
			b:    []byte{0xDE, 0xAD, 0xC0, 0xDE, 0x00, 0x00},
			err:  errors.Wrap(errIncorrectSize, opUnStatus),
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {

			// Wrap in status attribute container
			nfa := netfilter.Attribute{
				Type: uint16(CTAStatus),
				Data: tt.b,
			}

			var s Status

			err := s.UnmarshalAttribute(nfa)
			if err != nil || tt.err != nil {
				require.Error(t, err)
				require.EqualError(t, tt.err, err.Error())
				return
			}

			if diff := cmp.Diff(tt.status.value, s.value); diff != "" {
				t.Fatalf("unexpected unmarshal (-want +got):\n%s", diff)
			}

			ms := s.MarshalAttribute()
			require.NoError(t, err, "error during marshal:", s)
			if diff := cmp.Diff(nfa, ms); diff != "" {
				t.Fatalf("unexpected marshal (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStatusFieldTest(t *testing.T) {

	var s Status

	s.Set(StatusExpected)
	assert.Equal(t, true, s.Expected(), "expected")

	s.Set(StatusSeenReply)
	assert.Equal(t, true, s.SeenReply(), "seenreply")

	s.Set(StatusAssured)
	assert.Equal(t, true, s.Assured(), "assured")

	s.Set(StatusConfirmed)
	assert.Equal(t, true, s.Confirmed(), "confirmed")

	s.Set(StatusSrcNAT)
	assert.Equal(t, true, s.SrcNAT(), "srcnat")

	s.Set(StatusDstNAT)
	assert.Equal(t, true, s.DstNAT(), "dstnat")

	s.Set(StatusSeqAdjust)
	assert.Equal(t, true, s.SeqAdjust(), "seqadjust")

	s.Set(StatusSrcNATDone)
	assert.Equal(t, true, s.SrcNATDone(), "srcnatdone")

	s.Set(StatusDstNATDone)
	assert.Equal(t, true, s.DstNATDone(), "dstnatdone")

	s.Set(StatusDying)
	assert.Equal(t, true, s.Dying(), "dying")

	s.Set(StatusFixedTimeout)
	assert.Equal(t, true, s.FixedTimeout(), "fixedtimeout")

	s.Set(StatusTemplate)
	assert.Equal(t, true, s.Template(), "template")

	s.Set(StatusHelper)
	assert.Equal(t, true, s.Helper(), "helper")

	s.Set(StatusOffload)
	assert.Equal(t, true, s.Offload(), "offload")
}

func TestStatusString(t *testing.T) {
	full := Status{value: 0xffffffff}
	empty := Status{}

	wantFull := "EXPECTED|SEEN_REPLY|ASSURED|CONFIRMED|SRC_NAT|DST_NAT|SEQ_ADJUST|SRC_NAT_DONE|DST_NAT_DONE|DYING|FIXED_TIMEOUT|TEMPLATE|UNTRACKED|HELPER|OFFLOAD"
	if want, got := wantFull, full.String(); want != got {
		t.Errorf("unexpected string:\n- want: %s\n-  got: %s", wantFull, got)
	}

	wantEmpty := "NONE"
	if want, got := wantEmpty, empty.String(); wantEmpty != got {
		t.Errorf("unexpected string:\n- want: %s\n-  got: %s", want, got)
	}

}

func BenchmarkStatus_UnmarshalAttribute(b *testing.B) {
	inputs := [][]byte{
		{0x00, 0x00, 0x00, 0x01}, {0x00, 0x00, 0x00, 0x02}, {0x00, 0x00, 0x00, 0x03}, {0x00, 0x00, 0x00, 0x04},
		{0x00, 0x00, 0x00, 0x05}, {0x00, 0x00, 0x00, 0x06}, {0x00, 0x00, 0x00, 0x07}, {0x00, 0x00, 0x00, 0x08},
	}

	var ss Status
	var nfa netfilter.Attribute
	nfa.Type = uint16(CTAStatus)

	for n := 0; n < b.N; n++ {
		nfa.Data = inputs[n%len(inputs)]
		if err := (&ss).UnmarshalAttribute(nfa); err != nil {
			b.Fatal(err)
		}
	}
}
