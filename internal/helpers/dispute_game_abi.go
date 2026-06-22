package helpers

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// minimalDisputeGameABI carries just enough of the DisputeGameFactory + the
// FaultDisputeGame proxy interface for FindCoveringDisputeGame:
//
//   - factory.gameCount() -> uint256
//   - factory.gameAtIndex(uint256) -> (uint32 gameType, uint64 timestamp, address proxy)
//   - gameProxy.extraData() -> bytes
//
// Several configs don't ship a `dispute-game-factory` entry at all
// (sepolia-stage, sepolia-prod) and the one that does (hoodi) has the
// DisputeGame ABI under that key rather than the factory ABI — neither
// exposes `gameCount`, so the global DisputeGameABI populated by setup() is
// effectively empty for these calls. This self-contained minimal ABI keeps
// the L2->L1 finalize tests portable across every env without needing the
// configs to ship the right ABI.
const minimalDisputeGameABI = `[
	{"type":"function","name":"gameCount","inputs":[],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
	{"type":"function","name":"gameAtIndex","inputs":[{"name":"_index","type":"uint256"}],"outputs":[{"type":"uint32"},{"type":"uint64"},{"type":"address"}],"stateMutability":"view"},
	{"type":"function","name":"extraData","inputs":[],"outputs":[{"type":"bytes"}],"stateMutability":"view"}
]`

// MinimalDisputeGameABI is the parsed minimalDisputeGameABI, ready to pass
// to FindCoveringDisputeGame for both the factory and game arguments.
var MinimalDisputeGameABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(minimalDisputeGameABI))
	if err != nil {
		panic("parse minimalDisputeGameABI: " + err.Error())
	}
	return parsed
}()
