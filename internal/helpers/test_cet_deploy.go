package helpers

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/transactions"
)

// TestCETABI is the ABI for the test-only ComposableERC20-compatible token
// used by the redeemWrappedCET E2E test. Source lives at /tmp/dome-cet/TestCET.sol;
// the bytecode below was produced by `solc 0.8.35 --optimize` from that file.
const TestCETABI = `[
	{"type":"constructor","inputs":[{"name":"_name","type":"string"},{"name":"_symbol","type":"string"},{"name":"remoteAsset_","type":"address"},{"name":"remoteChainID_","type":"uint256"},{"name":"cetType_","type":"uint8"},{"name":"bridge","type":"address"}],"stateMutability":"nonpayable"},
	{"type":"function","name":"name","inputs":[],"outputs":[{"type":"string"}],"stateMutability":"view"},
	{"type":"function","name":"symbol","inputs":[],"outputs":[{"type":"string"}],"stateMutability":"view"},
	{"type":"function","name":"decimals","inputs":[],"outputs":[{"type":"uint8"}],"stateMutability":"view"},
	{"type":"function","name":"totalSupply","inputs":[],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
	{"type":"function","name":"balanceOf","inputs":[{"name":"","type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
	{"type":"function","name":"allowance","inputs":[{"name":"","type":"address"},{"name":"","type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
	{"type":"function","name":"authorizedBridges","inputs":[{"name":"","type":"address"}],"outputs":[{"type":"bool"}],"stateMutability":"view"},
	{"type":"function","name":"remoteAsset","inputs":[],"outputs":[{"type":"address"}],"stateMutability":"view"},
	{"type":"function","name":"remoteChainID","inputs":[],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
	{"type":"function","name":"cetType","inputs":[],"outputs":[{"type":"uint8"}],"stateMutability":"view"},
	{"type":"function","name":"mint","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},
	{"type":"function","name":"crosschainMint","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},
	{"type":"function","name":"crosschainBurn","inputs":[{"name":"from","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},
	{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}],"anonymous":false},
	{"type":"event","name":"Approval","inputs":[{"name":"owner","type":"address","indexed":true},{"name":"spender","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}],"anonymous":false}
]`

// TestCETBytecode is the deploy bytecode for TestCET.sol. Regenerate with:
//
//	solc 0.8.35+ --optimize --optimize-runs 200 --bin /tmp/dome-cet/TestCET.sol
//
// CetType: 0 = CORE (remoteAsset = self, remoteChainID = block.chainid),
// 1 = WRAPPED (remoteAsset/remoteChainID taken from constructor args).
const TestCETBytecode = "0x608060405234801561000f575f5ffd5b5060405161098a38038061098a83398101604081905261002e91610181565b5f61003987826102b4565b50600161004686826102b4565b508160ff165f0361006c57600580546001600160a01b031916301790554660065561008d565b600580546001600160a01b0319166001600160a01b03861617905560068390555b6007805460ff90931660ff199384161790556001600160a01b03165f908152600860205260409020805490911660011790555061037292505050565b634e487b7160e01b5f52604160045260245ffd5b5f82601f8301126100ec575f5ffd5b81516001600160401b03811115610105576101056100c9565b604051601f8201601f19908116603f011681016001600160401b0381118282101715610133576101336100c9565b60405281815283820160200185101561014a575f5ffd5b8160208501602083015e5f918101602001919091529392505050565b80516001600160a01b038116811461017c575f5ffd5b919050565b5f5f5f5f5f5f60c08789031215610196575f5ffd5b86516001600160401b038111156101ab575f5ffd5b6101b789828a016100dd565b602089015190975090506001600160401b038111156101d4575f5ffd5b6101e089828a016100dd565b9550506101ef60408801610166565b935060608701519250608087015160ff8116811461020b575f5ffd5b915061021960a08801610166565b90509295509295509295565b600181811c9082168061023957607f821691505b60208210810361025757634e487b7160e01b5f52602260045260245ffd5b50919050565b601f8211156102af57828211156102af57805f5260205f20601f840160051c602085101561028857505f5b90810190601f840160051c035f5b818110156102ab575f83820155600101610296565b5050505b505050565b81516001600160401b038111156102cd576102cd6100c9565b6102e1816102db8454610225565b8461025d565b6020601f821160018114610313575f83156102fc5750848201515b5f19600385901b1c1916600184901b17845561036b565b5f84815260208120601f198516915b828110156103425787850151825560209485019460019092019101610322565b508482101561035f57868401515f19600387901b60f8161c191681555b505060018360011b0184555b5050505050565b61060b8061037f5f395ff3fe608060405234801561000f575f5ffd5b50600436106100cb575f3560e01c806340c10f19116100885780637bb91f3e116100635780637bb91f3e146101c557806395d89b41146101d0578063d2382242146101d8578063dd62ed3e146101e0575f5ffd5b806340c10f19146101615780636fc063be1461017457806370a08231146101a6575f5ffd5b806306fdde03146100cf57806318160ddd146100ed57806318bf50771461010457806319350367146101195780632b8c49e314610134578063313ce56714610147575b5f5ffd5b6100d761020a565b6040516100e49190610494565b60405180910390f35b6100f660045481565b6040519081526020016100e4565b6101176101123660046104e4565b610295565b005b6005546040516001600160a01b0390911681526020016100e4565b6101176101423660046104e4565b61036e565b61014f601281565b60405160ff90911681526020016100e4565b61011761016f3660046104e4565b6102e5565b61019661018236600461050c565b60086020525f908152604090205460ff1681565b60405190151581526020016100e4565b6100f66101b436600461050c565b60026020525f908152604090205481565b60075460ff1661014f565b6100d7610487565b6006546100f6565b6100f66101ee36600461052c565b600360209081525f928352604080842090915290825290205481565b5f80546102169061055d565b80601f01602080910402602001604051908101604052809291908181526020018280546102429061055d565b801561028d5780601f106102645761010080835404028352916020019161028d565b820191905f5260205f20905b81548152906001019060200180831161027057829003601f168201915b505050505081565b335f9081526008602052604090205460ff166102e55760405162461bcd60e51b81526004016102dc906020808252600490820152630c2eae8d60e31b604082015260600190565b60405180910390fd5b6001600160a01b0382165f908152600260205260408120805483929061030c9084906105a9565b925050819055508060045f82825461032491906105a9565b90915550506040518181526001600160a01b038316905f907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef906020015b60405180910390a35050565b335f9081526008602052604090205460ff166103b55760405162461bcd60e51b81526004016102dc906020808252600490820152630c2eae8d60e31b604082015260600190565b6001600160a01b0382165f908152600260205260409020548111156104065760405162461bcd60e51b815260206004820152600760248201526662616c616e636560c81b60448201526064016102dc565b6001600160a01b0382165f908152600260205260408120805483929061042d9084906105c2565b925050819055508060045f82825461044591906105c2565b90915550506040518181525f906001600160a01b038416907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef90602001610362565b600180546102169061055d565b602081525f82518060208401528060208501604085015e5f604082850101526040601f19601f83011684010191505092915050565b80356001600160a01b03811681146104df575f5ffd5b919050565b5f5f604083850312156104f5575f5ffd5b6104fe836104c9565b946020939093013593505050565b5f6020828403121561051c575f5ffd5b610525826104c9565b9392505050565b5f5f6040838503121561053d575f5ffd5b610546836104c9565b9150610554602084016104c9565b90509250929050565b600181811c9082168061057157607f821691505b60208210810361058f57634e487b7160e01b5f52602260045260245ffd5b50919050565b634e487b7160e01b5f52601160045260245ffd5b808201808211156105bc576105bc610595565b92915050565b818103818111156105bc576105bc61059556fea2646970667358221220c274144f675dc7f6ed51be359c8cb6bd0bed5bcf5da178f1b03beac9cc33523064736f6c63430008230033"

// TestCETType identifies CORE vs WRAPPED for the deploy helper.
type TestCETType uint8

const (
	TestCETTypeCORE    TestCETType = 0
	TestCETTypeWRAPPED TestCETType = 1
)

// ParseTestCETABI returns the parsed TestCET ABI.
func ParseTestCETABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(TestCETABI))
}

// DeployTestCET deploys a TestCET on the account's rollup.
//
// For CORE deployments pass remoteAsset=common.Address{} and remoteChainID=nil;
// the constructor overrides them to (address(this), block.chainid).
// For WRAPPED deployments pass the address+chain ID of the core CET this wrapper
// represents.
//
// `bridge` is authorized for crosschainMint/Burn on the deployed contract.
func DeployTestCET(
	ctx context.Context,
	ac *accounts.Account,
	name string,
	symbol string,
	remoteAsset common.Address,
	remoteChainID *big.Int,
	cetType TestCETType,
	bridge common.Address,
) (common.Address, *types.Transaction, error) {
	parsedABI, err := ParseTestCETABI()
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("parse test-cet abi: %w", err)
	}
	if remoteChainID == nil {
		remoteChainID = big.NewInt(0)
	}
	constructorArgs, err := parsedABI.Pack("", name, symbol, remoteAsset, remoteChainID, uint8(cetType), bridge)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("pack test-cet constructor: %w", err)
	}

	bytecode, err := hexutil.Decode(TestCETBytecode)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("decode test-cet bytecode: %w", err)
	}
	deployData := append(bytecode, constructorArgs...)

	nonce, err := ac.GetNonce(ctx)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("nonce: %w", err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   ac.GetRollup().ChainID(),
		Nonce:     nonce,
		To:        nil,
		Gas:       2_000_000,
		GasTipCap: GasTipCap,
		GasFeeCap: GasFeeCap,
		Value:     big.NewInt(0),
		Data:      deployData,
	})
	signed, err := types.SignTx(tx, types.NewLondonSigner(ac.GetRollup().ChainID()), ac.GetPrivateKey())
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("sign test-cet deploy: %w", err)
	}
	if _, err := transactions.SendTransaction(ctx, signed, ac.GetRollup().RPCURL()); err != nil {
		return common.Address{}, nil, fmt.Errorf("send test-cet deploy: %w", err)
	}
	_, receipt, err := transactions.GetTransactionDetails(ctx, signed.Hash(), ac.GetRollup())
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("get test-cet deploy receipt: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Address{}, nil, fmt.Errorf("test-cet deploy failed: %s", signed.Hash().Hex())
	}
	addr := receipt.ContractAddress
	if addr == (common.Address{}) {
		addr = crypto.CreateAddress(ac.GetAddress(), nonce)
	}
	return addr, signed, nil
}
