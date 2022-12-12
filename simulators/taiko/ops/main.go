package main

import (
	"context"
	"math/big"
	"math/rand"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/taiko"
	"github.com/stretchr/testify/require"
	"github.com/taikoxyz/taiko-client/bindings"
	"github.com/taikoxyz/taiko-client/pkg/rpc"
	"github.com/taikoxyz/taiko-client/testutils"
)

func main() {
	suit := hivesim.Suite{
		Name:        "taiko ops",
		Description: "Test propose, sync and other things",
	}
	suit.Add(&hivesim.TestSpec{
		Name:        "single node net ops",
		Description: "test ops on single node net",
		Run:         singleNodeTest,
	})
	suit.Add(&hivesim.TestSpec{
		Name:        "tooManyPendingBlocks",
		Description: "Too many pending blocks will block further proposes",
		Run:         tooManyPendingBlocks,
	})
	suit.Add(&hivesim.TestSpec{
		Name:        "proposeInvalidTxListBytes",
		Description: "Commits and proposes an invalid transaction list bytes to TaikoL1 contract.",
		Run:         proposeInvalidTxListBytes,
	})
	suit.Add(&hivesim.TestSpec{
		Name:        "proposeTxListIncludingInvalidTx",
		Description: "Commits and proposes a validly encoded transaction list which including an invalid transaction.",
		Run:         proposeTxListIncludingInvalidTx,
	})
	sim := hivesim.New()
	hivesim.MustRun(sim, suit)
}

func singleNodeTest(t *hivesim.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	env := taiko.NewTestEnv(ctx, t, taiko.DefaultConfig)
	env.StartSingleNodeNet(t)

	// generate the first L2 transaction
	env.L2Vault.CreateAccount(ctx, env.Net.GetL2ELNode(0).EthClient(t), big.NewInt(params.Ether))

	t.Run(hivesim.TestSpec{
		Name:        "first L1 block",
		Description: "",
		Run:         firstL1Block(t, env),
	})
	t.Run(hivesim.TestSpec{
		Name:        "firstVerifiedL2Block",
		Description: "watch prove event of the first L2 block on L1",
		Run:         firstVerifiedL2Block(t, env),
	})

	t.Run(hivesim.TestSpec{
		Name:        "sync from L1",
		Description: "completes sync purely from L1 data to generate L2 block",
		Run:         syncAllFromL1(t, env),
	})

	t.Run(hivesim.TestSpec{
		Name:        "sync by p2p",
		Description: "L2 chain head determined by L1, but sync block completes through taiko-geth P2P",
		Run:         syncByP2P(t, env),
	})
}

func firstL1Block(t *hivesim.T, env *taiko.TestEnv) func(*hivesim.T) {
	return func(t *hivesim.T) {
		taiko.GenCommitDelayBlocks(t, env)
		taiko.WaitHeight(env.Context, t, env.Net.GetL1ELNode(0).EthClient(t), taiko.Greater(common.Big0.Int64()))
	}
}

// wait the a L2 transaction be proposed and proved as a L2 block.
func firstVerifiedL2Block(t *hivesim.T, env *taiko.TestEnv) func(*hivesim.T) {
	return func(t *hivesim.T) {
		ctx, d := env.Context, env.Net
		blockHash := taiko.GetBlockHashByNumber(ctx, t, d.GetL2ELNode(0).EthClient(t), common.Big1, true)
		taiko.WaitProveEvent(ctx, t, d.GetL1ELNode(0), blockHash)
	}
}

func genInvalidL2Block(t *hivesim.T, evn *taiko.TestEnv) {
	// TODO
}

func l1Reorg(t *hivesim.T, env *taiko.TestEnv) {
	l1 := env.Net.GetL1ELNode(0)
	taikoL1 := l1.TaikoL1Client(t)
	l1State, err := rpc.GetProtocolStateVariables(taikoL1, nil)
	require.NoError(t, err)
	l1GethCli := l1.GethClient()
	require.NoError(t, l1GethCli.SetHead(env.Context, big.NewInt(int64(l1State.GenesisHeight))))
	l2 := env.Net.GetL2ELNode(0)
	taiko.WaitHeight(env.Context, t, l2.EthClient(t), taiko.Greater(-1))
}

// Start a new driver and taiko-geth, the driver is connected to L1 that already has a propose block,
// and the driver will synchronize and process the propose event on L1 to let taiko-geth generate a new block.
func syncAllFromL1(t *hivesim.T, env *taiko.TestEnv) func(*hivesim.T) {
	return func(t *hivesim.T) {
		ctx, d := env.Context, env.Net
		l2 := taiko.NewL2ELNode(t, env, "")
		taiko.NewDriverNode(t, env, d.GetL1ELNode(0), l2, false)
		taiko.WaitHeight(ctx, t, l2.EthClient(t), taiko.Greater(common.Big0.Int64()))
	}
}

func syncByP2P(t *hivesim.T, env *taiko.TestEnv) func(*hivesim.T) {
	return func(t *hivesim.T) {
		ctx, d := env.Context, env.Net
		l2 := d.GetL2ELNode(0).EthClient(t)
		l2LatestHeight, err := l2.BlockNumber(ctx)
		require.NoError(t, err)
		// generate more L2 transactions for test
		cnt := 2
		for i := 0; i < cnt; i++ {
			env.L2Vault.CreateAccount(ctx, l2, big.NewInt(params.Ether))
			taiko.WaitHeight(ctx, t, l2, taiko.Greater(int64(l2LatestHeight)+int64(i)))
		}
		// start new L2 engine and driver to sync by p2p
		newL2 := taiko.NewL2ELNode(t, env, d.GetL2ENodes(t))
		taiko.NewDriverNode(t, env, d.GetL1ELNode(0), newL2, true)

		taikoL1 := d.GetL1ELNode(0).TaikoL1Client(t)
		l1State, err := rpc.GetProtocolStateVariables(taikoL1, nil)
		require.NoError(t, err)
		if l1State.LatestVerifiedHeight > 0 {
			taiko.WaitHeight(ctx, t, newL2.EthClient(t), taiko.Greater(int64(l1State.LatestVerifiedHeight)))
		} else {
			t.Logf("sync by p2p, but LatestVerifiedHeight==0")
		}
	}
}

// Since there is no prover, state.LatestVerifiedId is always 0,
// so you will get an error when you propose the LibConstants.K_MAX_NUM_BLOCKS block
func tooManyPendingBlocks(t *hivesim.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	env := taiko.NewTestEnv(ctx, t, taiko.DefaultConfig)
	env.StartL1L2Driver(t)

	l1, l2 := env.Net.GetL1ELNode(0), env.Net.GetL2ELNode(0)

	prop := taiko.NewProposer(t, env, taiko.NewProposerConfig(env, l1, l2))

	taikoL1 := l1.TaikoL1Client(t)
	for canPropose(t, env, taikoL1) {
		require.NoError(t, env.L2Vault.SendTestTx(ctx, l2.EthClient(t)))
		require.NoError(t, prop.ProposeOp(env.Context))
		time.Sleep(10 * time.Millisecond)
	}
	// wait error
	require.NoError(t, env.L2Vault.SendTestTx(ctx, l2.EthClient(t)))
	err := prop.ProposeOp(env.Context)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "L1:tooMany"))
}

func canPropose(t *hivesim.T, env *taiko.TestEnv, taikoL1 *bindings.TaikoL1Client) bool {
	l1State, err := rpc.GetProtocolStateVariables(taikoL1, nil)
	require.NoError(t, err)
	return l1State.NextBlockID < l1State.LatestVerifiedID+env.L1Constants.MaxNumBlocks.Uint64()
}

// proposeInvalidTxListBytes commits and proposes an invalid transaction list
// bytes to TaikoL1 contract.
func proposeInvalidTxListBytes(t *hivesim.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	env := taiko.NewTestEnv(ctx, t, taiko.DefaultConfig)
	env.StartL1L2(t)

	l1, l2 := env.Net.GetL1ELNode(0), env.Net.GetL2ELNode(0)
	p := taiko.NewProposer(t, env, taiko.NewProposerConfig(env, l1, l2))

	invalidTxListBytes := testutils.RandomBytes(256)
	meta, commitTx, err := p.CommitTxList(
		env.Context,
		invalidTxListBytes,
		uint64(rand.Int63n(env.L1Constants.BlockMaxGasLimit.Int64())),
		0,
	)
	require.NoError(t, err)
	taiko.GenCommitDelayBlocks(t, env)
	require.Nil(t, p.ProposeTxList(env.Context, meta, commitTx, invalidTxListBytes, 1))
	taiko.WaitHeight(ctx, t, l1.EthClient(t), taiko.Greater(0))
	taiko.WaitStateChange(t, l1.TaikoL1Client(t), func(psv *bindings.ProtocolStateVariables) bool {
		if psv.NextBlockID == 2 {
			return true
		}
		return false
	})
}

// proposeTxListIncludingInvalidTx commits and proposes a validly encoded
// transaction list which including an invalid transaction.
func proposeTxListIncludingInvalidTx(t *hivesim.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	env := taiko.NewTestEnv(ctx, t, taiko.DefaultConfig)
	env.StartL1L2Driver(t)

	l1, l2 := env.Net.GetL1ELNode(0), env.Net.GetL2ELNode(0)
	p := taiko.NewProposer(t, env, taiko.NewProposerConfig(env, l1, l2))

	invalidTx := generateInvalidTransaction(t, env)

	txListBytes, err := rlp.EncodeToBytes(types.Transactions{invalidTx})
	require.NoError(t, err)

	meta, commitTx, err := p.CommitTxList(env.Context, txListBytes, invalidTx.Gas(), 0)
	require.NoError(t, err)

	taiko.GenCommitDelayBlocks(t, env)

	require.Nil(t, p.ProposeTxList(env.Context, meta, commitTx, txListBytes, 1))

	taiko.WaitHeight(ctx, t, l1.EthClient(t), taiko.Greater(0))
	taiko.WaitStateChange(t, l1.TaikoL1Client(t), func(psv *bindings.ProtocolStateVariables) bool {
		if psv.NextBlockID == 2 {
			return true
		}
		return false
	})
	pendingNonce, err := l2.EthClient(t).PendingNonceAt(context.Background(), env.Conf.L2.Proposer.Address)
	require.Nil(t, err)
	require.NotEqual(t, invalidTx.Nonce(), pendingNonce)
}

// generateInvalidTransaction creates a transaction with an invalid nonce to
// current L2 world state.
func generateInvalidTransaction(t *hivesim.T, env *taiko.TestEnv) *types.Transaction {
	opts, err := bind.NewKeyedTransactorWithChainID(env.Conf.L2.Proposer.PrivateKey, env.Conf.L2.ChainID)
	require.NoError(t, err)
	l2 := env.Net.GetL2ELNode(0)
	nonce, err := l2.EthClient(t).PendingNonceAt(env.Context, env.Conf.L2.Proposer.Address)
	require.NoError(t, err)

	opts.GasLimit = 300000
	opts.NoSend = true
	opts.Nonce = new(big.Int).SetUint64(nonce + 1024)

	taikoL2 := l2.TaikoL2Client(t)
	tx, err := taikoL2.Anchor(opts, common.Big0, common.BytesToHash(testutils.RandomBytes(32)))
	require.NoError(t, err)
	return tx
}
