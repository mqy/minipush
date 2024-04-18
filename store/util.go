package store

import (
	"encoding/base64"
	"time"

	pb "github.com/mqy/minipush/proto"
)

// GetDayBefore get the time of before `days`, exclude today.
func GetDayBefore(days int32) time.Time {
	days += 1
	offset := time.Duration(days*24) * time.Hour
	d := time.Now().Add(-offset)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
}

func MakeGetReadStatesResp(seqSlice []int32, readStates []bool) (*pb.GetReadStatesResp, error) {
	resp := &pb.GetReadStatesResp{}
	if len(seqSlice) == 0 || len(readStates) == 0 {
		return resp, nil
	}

	var blockAt []int
	var lastSeq int32 = -1

	for i := 0; i < len(seqSlice); i++ {
		seq := seqSlice[i]
		if seq > lastSeq+1 {
			blockAt = append(blockAt, i)
		}
		lastSeq = seq
	}

	var blocks []*pb.GetReadStatesResp_Block

	numBlocks := len(blockAt)
	var end int
	for i, start := range blockAt {
		if i+1 < numBlocks {
			end = blockAt[i+1]
		} else { // last block
			end = len(readStates)
		}

		boolSlice := readStates[start:end]
		byteSlice := boolSliceToBytes(boolSlice)
		encoded := base64.StdEncoding.EncodeToString(byteSlice)
		blocks = append(blocks, &pb.GetReadStatesResp_Block{
			Seq:    seqSlice[start],
			Len:    int32(len(boolSlice)),
			Base64: encoded,
		})
	}
	resp.Blocks = blocks
	return resp, nil
}

// boolSliceToBytes converts given slice into byte array, BIG endian.
// Example: false, true, true, true, false, false, true, true, true => 0b01110011, 0b10000000
func boolSliceToBytes(slice []bool) []byte {
	numBytes := len(slice) / 8
	if len(slice)%8 > 0 {
		numBytes++
	}
	byteSlice := make([]byte, numBytes)
	for i, v := range slice {
		x := i / 8
		y := i % 8
		if v {
			byteSlice[x] |= 1 << (7 - y)
		}
	}
	return byteSlice
}
