export const wallet_private_key = "a332c2c156a462851f1f62207311ab05e406a9523c58e5eda86e27d0649ce886"

// =============================================================================
// SEPOLIA PROD (from sepolia-prod/)
// =============================================================================

// Sepolia L1 RPC
export const SEPOLIA_L1_RPC = "http://141.95.35.120:31070"
export const SEPOLIA_Rollup_A_RPC = "https://rpc-a-altda.sepolia.ethera-labs.io/"
export const SEPOLIA_Rollup_B_RPC = "https://rpc-b-altda.sepolia.ethera-labs.io/"
export const SEPOLIA_SIDECAR_URL = "" // not used for sepolia-prod — XT goes to rollup RPC

// Sepolia chain IDs
export const SEPOLIA_L1_CHAIN_ID = 11155111
export const SEPOLIA_ROLLUP_A_CHAIN_ID = 555555
export const SEPOLIA_ROLLUP_B_CHAIN_ID = 666666

// Sepolia L1 addresses (from sepolia-prod/L1/addresses.toml)
export const SEPOLIA_COMPOSE_L1_BRIDGE_ROLLUP_A = "0xF3504fc6AAB6Da84cc466bB707a109a8824a0c24"
export const SEPOLIA_COMPOSE_L1_BRIDGE_ROLLUP_B = "0x30Fdb3B0035828372a9Df4D2F6B66CA27529C9B0"
export const SEPOLIA_COMPOSE_PORTAL_ROLLUP_A = "0xbc2e5a158b9d3ea5ff145ea261736ab0ca6517f9"
export const SEPOLIA_COMPOSE_PORTAL_ROLLUP_B = "0xd32ed6c0a353ae79e087890d0f86d53d78036055"

// Sepolia L2 addresses (from sepolia-prod/L2/addresses.toml)
export const SEPOLIA_CET_FACTORY = "0x3E42f25a249b51F1822E6aB913BeF666769D87cd"
export const SEPOLIA_COMPOSE_L2_TO_L2_BRIDGE = "0x57d93F3E2fD17E3e42A24F113D5728441Bf79cb3"
export const SEPOLIA_UNIVERSAL_BRIDGE_MAILBOX = "0x3455060f6e051161241BcFb19d9dc4048B328A63"
export const SEPOLIA_COMPOSE_L2_BRIDGE_ROLLUP_A = "0xa48CC954531A24c737f496C60daa44ABAd8475ab"
export const SEPOLIA_COMPOSE_L2_BRIDGE_ROLLUP_B = "0xcF29d49571A5Cd7350AE27951993e57a89743599"

// Sepolia DEX tokens on RollupA
export const SEPOLIA_ROLLUP_A_USDC = "0x556fBe60491F59F1C855c465313a398Ee6ba4729"

// Sepolia AA contracts (from @ssv-labs/ethera-sdk rollupsAccountAbstractionContracts)
export const SEPOLIA_AA_KERNEL_IMPL = "0xBAC849bB641841b44E965fB01A4Bf5F074f84b4D"
export const SEPOLIA_AA_KERNEL_FACTORY = "0xaac5D4240AF87249B3f71BC8E4A2cae074A3E419"
export const SEPOLIA_AA_MULTICHAIN_VALIDATOR = "0x37CE732412539644b3d0E959925a4f89edd463c9"

// =============================================================================
// SEPOLIA STAGE (from sepolia-stage/)
// =============================================================================

// Sepolia-stage RPCs
export const SEPOLIA_STAGE_L1_RPC = "http://141.95.35.120:31070" // same Sepolia L1
export const SEPOLIA_STAGE_Rollup_A_RPC = "https://op-rbuilder-a.stage.ethera-labs.io"
export const SEPOLIA_STAGE_Rollup_B_RPC = "https://op-rbuilder-b.stage.ethera-labs.io"
export const SEPOLIA_STAGE_SIDECAR_URL = "http://127.0.0.1:18080/xt" // XT requests go here

// Sepolia-stage chain IDs (from sepolia-stage/L1/addresses.toml)
export const SEPOLIA_STAGE_L1_CHAIN_ID = 11155111
export const SEPOLIA_STAGE_ROLLUP_A_CHAIN_ID = 100003
export const SEPOLIA_STAGE_ROLLUP_B_CHAIN_ID = 200005

// Sepolia-stage L1 addresses (from sepolia-stage/L1/addresses.toml)
export const SEPOLIA_STAGE_COMPOSE_L1_BRIDGE_ROLLUP_A = "0xa61a95a393109523F80a88F4Db218Ec8531cC9e0"
export const SEPOLIA_STAGE_COMPOSE_L1_BRIDGE_ROLLUP_B = "0x3818672dc5Ad57c88B3bcC6A2e3846E545Fea69d"
export const SEPOLIA_STAGE_COMPOSE_PORTAL_ROLLUP_A = "0x08DcE4e7C40b87E111C98f2A359D158e6dcecd62"
export const SEPOLIA_STAGE_COMPOSE_PORTAL_ROLLUP_B = "0x5A9A603D837d66fFBA8e8e5487a09D80648c80af"

// Sepolia-stage L2 addresses (from sepolia-stage/L2/addresses.toml)
export const SEPOLIA_STAGE_CET_FACTORY = "0x552e0fc7105bd628e507ead8973694cf559ad27e"
export const SEPOLIA_STAGE_COMPOSE_L2_TO_L2_BRIDGE = "0x6e166073b5dd5fd53b33fed5a7bd9c104c6c6ebd"
export const SEPOLIA_STAGE_UNIVERSAL_BRIDGE_MAILBOX = "0xe7cd64151011ae6ad84cf96c107b61024d2071f0"
export const SEPOLIA_STAGE_COMPOSE_L2_BRIDGE_ROLLUP_A = "0x7b95c7780cfdaebe3d9397399fbdd2b5ad243aeb"
export const SEPOLIA_STAGE_COMPOSE_L2_BRIDGE_ROLLUP_B = "0x7b95c7780cfdaebe3d9397399fbdd2b5ad243aeb"
export const SEPOLIA_STAGE_COMPOSE_ETH_LIQUIDITY = "0x053402b5adfa240fe999a5080121ce53b51fa2c1"

// Sepolia-stage AA contracts (from sepolia-stage/L2/addresses.toml)
export const SEPOLIA_STAGE_AA_KERNEL_IMPL = "0xBAC849bB641841b44E965fB01A4Bf5F074f84b4D"
export const SEPOLIA_STAGE_AA_KERNEL_FACTORY = "0xaac5D4240AF87249B3f71BC8E4A2cae074A3E419"
export const SEPOLIA_STAGE_AA_MULTICHAIN_VALIDATOR = "0x37CE732412539644b3d0E959925a4f89edd463c9"

// Sepolia-stage bundler RPCs (AA/compose-aware — supports ethera_buildSignedUserOpsTx)
export const SEPOLIA_STAGE_BUNDLER_A_RPC = "https://bundler-a.stage.ethera-labs.io"
export const SEPOLIA_STAGE_BUNDLER_B_RPC = "https://bundler-b.stage.ethera-labs.io"

// =============================================================================
// HOODI PROD (from hoodi-prod/)
// =============================================================================

// Hoodi L1 RPC
export const HOODI_L1_RPC = "http://hoodi-geth-lh-1-execution.production.vnet.ops.ssvlabsinternal.com"
export const HOODI_Rollup_A_RPC = "https://rpc-a.testnet.compose.network/"
export const HOODI_Rollup_B_RPC = "https://rpc-b.testnet.compose.network/"
export const HOODI_SIDECAR_URL = "" // not used for hoodi — XT goes to rollup RPC

// Hoodi chain IDs
export const HOODI_L1_CHAIN_ID = 560048
export const HOODI_ROLLUP_A_CHAIN_ID = 11113
export const HOODI_ROLLUP_B_CHAIN_ID = 22224

// Hoodi L1 addresses (from hoodi-prod/L1/addresses.toml)
export const HOODI_COMPOSE_L1_BRIDGE_ROLLUP_A = "0x9e51839f96BcBDA61052dCd687002d3B2D4d083a"
export const HOODI_COMPOSE_L1_BRIDGE_ROLLUP_B = "0x8e1Dc5e261f0bB1A4193b9FCc9f270DfC4EBBf26"
export const HOODI_COMPOSE_PORTAL_ROLLUP_A = "0x5bd5bacb743643e5acf61dc2e9d7c4722810c447"
export const HOODI_COMPOSE_PORTAL_ROLLUP_B = "0x3673e03ac96f61fed037a53c6a626d4a798f67ff"
export const HOODI_ETH_LOCKBOX = "0x172eFBBc66dFc4e28f8F4E207ABdFE6eff3A3f60"
export const HOODI_ERC20_LOCKBOX = "0x5D8D58C1C295D50dD23aC8f43A4F4440177022b3"
export const HOODI_DISPUTE_GAME_FACTORY = "0xaFd9977Ab27683924dB2326Fd62a7d76443A70cC"

// Hoodi L2 addresses (from hoodi-prod/l2/addresses.toml)
export const HOODI_CET_FACTORY = "0x763E46B5DE482e8308bC7339a204B71c1D6D15c2"
export const HOODI_COMPOSE_L2_TO_L2_BRIDGE = "0x0aa490f4D727DE37e6355e417D25366342cB8f64"
export const HOODI_UNIVERSAL_BRIDGE_MAILBOX = "0x939F91F5e7d2a5FF4616A9415682CBF775596997"
export const HOODI_COMPOSE_L2_BRIDGE_ROLLUP_A = "0x63dD01577F7359048cf541502c1D56D6ee3767D3"
export const HOODI_COMPOSE_L2_BRIDGE_ROLLUP_B = "0xF11b0010088950627737c22C9331fA4DE6B75a6F"
export const HOODI_COMPOSE_ETH_LIQUIDITY = "0xE5E7F8d750581707DD2902514BD6458e740AF761"

// Hoodi AA (Account Abstraction) contracts — same on both rollups
export const HOODI_AA_KERNEL_IMPL = "0x317A2D4564778A585BAd21376dC1ca65b75ccC6a"
export const HOODI_AA_KERNEL_FACTORY = "0xdEF4343958B5dE047bddEFaB5Fa8F9Ff898890e5"
export const HOODI_AA_MULTICHAIN_VALIDATOR = "0x8aB3f6935399e1c10419cA2C93d60901a256b7e3"

// =============================================================================
// ACTIVE NETWORK — set via BRIDGE_NETWORK env var
// Supported values: "hoodi" (default), "sepolia", "sepolia-stage"
// Override by running: BRIDGE_NETWORK=sepolia-stage npx ts-node scripts/...
// =============================================================================
const NETWORK = process.env.BRIDGE_NETWORK?.toLowerCase() || "hoodi"

function selectConfig<T>(hoodi: T, sepolia: T, sepoliaStage: T): T {
  if (NETWORK === "hoodi") return hoodi
  if (NETWORK === "sepolia-stage") return sepoliaStage
  return sepolia // default fallback = sepolia prod
}

export const L1_RPC = selectConfig(HOODI_L1_RPC, SEPOLIA_L1_RPC, SEPOLIA_STAGE_L1_RPC)
export const L1_Rollup_1_RPC = selectConfig(HOODI_Rollup_A_RPC, SEPOLIA_Rollup_A_RPC, SEPOLIA_STAGE_Rollup_A_RPC)
export const L1_Rollup_2_RPC = selectConfig(HOODI_Rollup_B_RPC, SEPOLIA_Rollup_B_RPC, SEPOLIA_STAGE_Rollup_B_RPC)
export const L1_CHAIN_ID = selectConfig(HOODI_L1_CHAIN_ID, SEPOLIA_L1_CHAIN_ID, SEPOLIA_STAGE_L1_CHAIN_ID)
export const ROLLUP_A_CHAIN_ID = selectConfig(HOODI_ROLLUP_A_CHAIN_ID, SEPOLIA_ROLLUP_A_CHAIN_ID, SEPOLIA_STAGE_ROLLUP_A_CHAIN_ID)
export const ROLLUP_B_CHAIN_ID = selectConfig(HOODI_ROLLUP_B_CHAIN_ID, SEPOLIA_ROLLUP_B_CHAIN_ID, SEPOLIA_STAGE_ROLLUP_B_CHAIN_ID)
export const COMPOSE_L1_BRIDGE_ROLLUP_A = selectConfig(HOODI_COMPOSE_L1_BRIDGE_ROLLUP_A, SEPOLIA_COMPOSE_L1_BRIDGE_ROLLUP_A, SEPOLIA_STAGE_COMPOSE_L1_BRIDGE_ROLLUP_A)
export const COMPOSE_L1_BRIDGE_ROLLUP_B = selectConfig(HOODI_COMPOSE_L1_BRIDGE_ROLLUP_B, SEPOLIA_COMPOSE_L1_BRIDGE_ROLLUP_B, SEPOLIA_STAGE_COMPOSE_L1_BRIDGE_ROLLUP_B)
export const CET_FACTORY = selectConfig(HOODI_CET_FACTORY, SEPOLIA_CET_FACTORY, SEPOLIA_STAGE_CET_FACTORY)
export const COMPOSE_L2_TO_L2_BRIDGE = selectConfig(HOODI_COMPOSE_L2_TO_L2_BRIDGE, SEPOLIA_COMPOSE_L2_TO_L2_BRIDGE, SEPOLIA_STAGE_COMPOSE_L2_TO_L2_BRIDGE)
export const UNIVERSAL_BRIDGE_MAILBOX = selectConfig(HOODI_UNIVERSAL_BRIDGE_MAILBOX, SEPOLIA_UNIVERSAL_BRIDGE_MAILBOX, SEPOLIA_STAGE_UNIVERSAL_BRIDGE_MAILBOX)
export const ROLLUP_A_USDC = selectConfig("", SEPOLIA_ROLLUP_A_USDC, "")
export const AA_KERNEL_IMPL = selectConfig(HOODI_AA_KERNEL_IMPL, SEPOLIA_AA_KERNEL_IMPL, SEPOLIA_STAGE_AA_KERNEL_IMPL)
export const AA_KERNEL_FACTORY = selectConfig(HOODI_AA_KERNEL_FACTORY, SEPOLIA_AA_KERNEL_FACTORY, SEPOLIA_STAGE_AA_KERNEL_FACTORY)
export const AA_MULTICHAIN_VALIDATOR = selectConfig(HOODI_AA_MULTICHAIN_VALIDATOR, SEPOLIA_AA_MULTICHAIN_VALIDATOR, SEPOLIA_STAGE_AA_MULTICHAIN_VALIDATOR)
export const COMPOSE_L2_BRIDGE_ROLLUP_A = selectConfig(HOODI_COMPOSE_L2_BRIDGE_ROLLUP_A, SEPOLIA_COMPOSE_L2_BRIDGE_ROLLUP_A, SEPOLIA_STAGE_COMPOSE_L2_BRIDGE_ROLLUP_A)
export const COMPOSE_L2_BRIDGE_ROLLUP_B = selectConfig(HOODI_COMPOSE_L2_BRIDGE_ROLLUP_B, SEPOLIA_COMPOSE_L2_BRIDGE_ROLLUP_B, SEPOLIA_STAGE_COMPOSE_L2_BRIDGE_ROLLUP_B)
export const COMPOSE_PORTAL_ROLLUP_A = selectConfig(HOODI_COMPOSE_PORTAL_ROLLUP_A, SEPOLIA_COMPOSE_PORTAL_ROLLUP_A, SEPOLIA_STAGE_COMPOSE_PORTAL_ROLLUP_A)
export const COMPOSE_PORTAL_ROLLUP_B = selectConfig(HOODI_COMPOSE_PORTAL_ROLLUP_B, SEPOLIA_COMPOSE_PORTAL_ROLLUP_B, SEPOLIA_STAGE_COMPOSE_PORTAL_ROLLUP_B)

// Sidecar URL — for sepolia-stage, XT (cross-chain) requests go to the sidecar instead of rollup RPC
// For hoodi and sepolia-prod, this is empty (XT goes to source rollup RPC)
export const SIDECAR_URL = selectConfig(HOODI_SIDECAR_URL, SEPOLIA_SIDECAR_URL, SEPOLIA_STAGE_SIDECAR_URL)

// Bundler RPCs — for sepolia-stage SA scripts. These support ethera_buildSignedUserOpsTx
// (renamed from compose_buildSignedUserOpsTx). Empty for hoodi/sepolia-prod where rollup RPCs handle compose.
export const BUNDLER_ROLLUP_A_RPC = selectConfig("", "", SEPOLIA_STAGE_BUNDLER_A_RPC)
export const BUNDLER_ROLLUP_B_RPC = selectConfig("", "", SEPOLIA_STAGE_BUNDLER_B_RPC)

export const ACTIVE_NETWORK = NETWORK
