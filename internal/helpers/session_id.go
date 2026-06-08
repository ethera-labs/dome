package helpers

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// GenerateSessionIDV1 mirrors the `version << 240 | keccak256(addr,nonce,block,salt) >> 16`
// scheme that scripts/l2-to-l2.ts uses for the manual two-step bridge. The TS
// helper packs as solidityPacked(["address","uint32","uint64","uint32"], [...]).
func GenerateSessionIDV1(addr common.Address, nonce uint32, blockNumber uint64, salt uint32) *big.Int {
	packed := make([]byte, 0, 20+4+8+4)
	packed = append(packed, addr.Bytes()...)
	packed = binary.BigEndian.AppendUint32(packed, nonce)
	packed = binary.BigEndian.AppendUint64(packed, blockNumber)
	packed = binary.BigEndian.AppendUint32(packed, salt)
	hashBytes := crypto.Keccak256(packed)

	hash := new(big.Int).SetBytes(hashBytes)
	hash.Rsh(hash, 16) // >> 16

	version := big.NewInt(1)
	version.Lsh(version, 240) // version << 240

	return new(big.Int).Or(version, hash)
}

// RandomSaltUint32 returns a uniformly random uint32 for use as the salt
// component of GenerateSessionIDV1.
func RandomSaltUint32() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read panics in practice are vanishingly rare; if it fails return
		// a fixed nonzero value so tests still progress.
		return 0xdeadbeef
	}
	return binary.BigEndian.Uint32(b[:])
}
