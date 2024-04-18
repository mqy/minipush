package store

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/mqy/minipush/proto"
)

func TestNewReadAck(t *testing.T) {
	seqSlice := []int32{1, 2, 4, 5, 7}
	readStates := []bool{false, true, true, true, false}
	out, err := MakeGetReadStatesResp(seqSlice, readStates)
	assert.NoError(t, err)

	expect := &pb.GetReadStatesResp{
		Blocks: []*pb.GetReadStatesResp_Block{
			{
				Seq:    int32(1),
				Len:    int32(2),
				Base64: "QA==",
			},
			{
				Seq:    int32(4),
				Len:    int32(2),
				Base64: "wA==",
			},
			{
				Seq:    int32(7),
				Len:    int32(1),
				Base64: "AA==",
			},
		},
	}
	assert.EqualValues(t, expect, out)

	bools := []bool{}
	const n = 10_000
	for i := 0; i < n; i++ {
		bools = append(bools, i%2 == 1)
	}
	byteSlice := boolSliceToBytes(bools)
	s := base64.StdEncoding.EncodeToString(byteSlice)
	fmt.Printf("%d bools: len(s): %d\n", n, len(s))
}

func TestBoolSliceToBytes(t *testing.T) {
	readStates := []bool{false, true, true, true, false, false, true, true, true}
	out := boolSliceToBytes(readStates)
	assert.EqualValues(t, []byte{0b01110011, 0b10000000}, out)
}
