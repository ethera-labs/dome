package helpers

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// LabelSendETH matches the on-chain label the source bridge writes into the
// mailbox for native-ETH cross-chain transfers.
const LabelSendETH = "SEND_ETH"

// PackBridgeEthTo encodes a call to ComposeL2ToL2Bridge.bridgeEthTo.
//   bridgeEthTo(uint256 sessionId, uint256 chainDest, address receiver)
func PackBridgeEthTo(
	bridgeABI abi.ABI,
	sessionID *big.Int,
	chainDest *big.Int,
	receiver common.Address,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeEthTo", sessionID, chainDest, receiver)
}

// PackReceiveETH encodes a call to ComposeL2ToL2Bridge.receiveETH.
//   receiveETH(MessageHeader)
// where MessageHeader is the same tuple defined in helpers/bridge_txs.go.
func PackReceiveETH(
	bridgeABI abi.ABI,
	chainSrc *big.Int,
	chainDest *big.Int,
	sourceBridge common.Address,
	receiver common.Address,
	sessionID *big.Int,
) ([]byte, error) {
	return bridgeABI.Pack("receiveETH", MessageHeader{
		ChainSrc:  chainSrc,
		ChainDest: chainDest,
		Sender:    sourceBridge,
		Receiver:  receiver,
		SessionId: sessionID,
		Label:     LabelSendETH,
	})
}
