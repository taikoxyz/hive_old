package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/taiko"
	"github.com/stretchr/testify/require"
)

type testSpec struct {
	Name  string
	About string
	Run   func(*LegacyTestEnv)
}

var tests = []testSpec{
	{Name: "http/BalanceAndNonceAt", Run: balanceAndNonceAtTest},
	{Name: "http/CanonicalChain", Run: canonicalChainTest},
	{Name: "http/CodeAt", Run: CodeAtTest},
	{Name: "http/ContractDeployment", Run: deployContractTest},
	{Name: "http/ContractDeploymentOutOfGas", Run: deployContractOutOfGasTest},
	{Name: "http/EstimateGas", Run: estimateGasTest},
	{Name: "http/GenesisBlockByHash", Run: genesisBlockByHashTest},
	{Name: "http/GenesisBlockByNumber", Run: genesisBlockByNumberTest},
	{Name: "http/GenesisHeaderByHash", Run: genesisHeaderByHashTest},
	{Name: "http/GenesisHeaderByNumber", Run: genesisHeaderByNumberTest},
	{Name: "http/Receipt", Run: receiptTest},
	{Name: "http/SyncProgress", Run: syncProgressTest},
	{Name: "http/TransactionCount", Run: transactionCountTest},
	{Name: "http/TransactionInBlock", Run: transactionInBlockTest},
	{Name: "http/TransactionReceipt", Run: TransactionReceiptTest},

	// HTTP ABI tests.
	{Name: "http/ABICall", Run: callContractTest},
	{Name: "http/ABITransact", Run: transactContractTest},

	// WebSocket ABI tests.
	{Name: "ws/ABICall", Run: callContractTest},
	{Name: "ws/ABITransact", Run: transactContractTest},

	// WebSocket subscription tests.
	{Name: "ws/NewHeadSubscription", Run: newHeadSubscriptionTest},
	{Name: "ws/LogSubscription", Run: logSubscriptionTest},
	{Name: "ws/TransactionInBlockSubscription", Run: transactionInBlockSubscriptionTest},

	// WebSocket RPC tests.
	{Name: "ws/BalanceAndNonceAt", Run: balanceAndNonceAtTest},
	{Name: "ws/CanonicalChain", Run: canonicalChainTest},
	{Name: "ws/CodeAt", Run: CodeAtTest},
	{Name: "ws/ContractDeployment", Run: deployContractTest},
	{Name: "ws/ContractDeploymentOutOfGas", Run: deployContractOutOfGasTest},
	{Name: "ws/EstimateGas", Run: estimateGasTest},
	{Name: "ws/GenesisBlockByHash", Run: genesisBlockByHashTest},
	{Name: "ws/GenesisBlockByNumber", Run: genesisBlockByNumberTest},
	{Name: "ws/GenesisHeaderByHash", Run: genesisHeaderByHashTest},
	{Name: "ws/GenesisHeaderByNumber", Run: genesisHeaderByNumberTest},
	{Name: "ws/Receipt", Run: receiptTest},
	{Name: "ws/SyncProgress", Run: syncProgressTest},
	{Name: "ws/TransactionCount", Run: transactionCountTest},
	{Name: "ws/TransactionInBlock", Run: transactionInBlockTest},
	{Name: "ws/TransactionReceipt", Run: TransactionReceiptTest},
}

func main() {
	suite := hivesim.Suite{
		Name: "taiko rpc",
		Description: `
The RPC test suite runs a set of RPC related tests against a running node. It tests
several real-world scenarios such as sending value transactions, deploying a contract or
interacting with one.`[1:],
	}

	// Add tests for full nodes.
	suite.Add(&hivesim.TestSpec{
		Name:        "client launch",
		Description: `This test launches the client and collects its logs.`,
		Run:         func(t *hivesim.T) { runAllTests(t) },
	})

	sim := hivesim.New()
	hivesim.MustRunSuite(sim, suite)
}

// runAllTests runs the tests against a client instance.
// Most tests simply wait for tx inclusion in a block so we can run many tests concurrently.
func runAllTests(t *hivesim.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	d := taiko.NewDevnet(t)
	require.NoError(t, taiko.StartDevnetWithSingleInstance(ctx, d, nil))
	l2 := d.GetL2(0)
	genesis := d.Genesis()
	// Need to adapt the tests a bit to work with the common
	// libraries in the taiko package.
	adaptedTests := make([]*taiko.TestSpec, len(tests))
	for i, test := range tests {
		adaptedTests[i] = &taiko.TestSpec{
			Name:        fmt.Sprintf("%s (%s)", test.Name, "taiko-l2"),
			Description: test.About,
			Run: func(t *hivesim.T, env *taiko.TestEnv) {
				switch test.Name[:strings.IndexByte(test.Name, '/')] {
				case "http":
					RunHTTP(t, l2.Geth.Client, d.L2Vault, genesis, test.Run)
				case "ws":
					RunWS(t, l2.Geth.Client, d.L2Vault, genesis, test.Run)
				default:
					panic("bad test prefix in name " + test.Name)
				}
			},
		}
	}
	taiko.RunTests(ctx, t, &taiko.RunTestsParams{
		Devnet:      d,
		Tests:       adaptedTests,
		Concurrency: 40,
	})
}
