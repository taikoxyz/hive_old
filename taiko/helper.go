package taiko

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/hive/hivesim"
)

func WaitELNodesUp(ctx context.Context, t *hivesim.T, nodes []*ELNode, timeout time.Duration) {
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, node *ELNode) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if _, err := n.EthClient().ChainID(ctx); err != nil {
				t.Fatalf("engine node %s_%d should be up within %v,err=%v", node.Type, i, timeout, err)
			}
		}(i, n)
	}
	wg.Wait()
}

func WaitBlock(ctx context.Context, client *ethclient.Client, n uint64) error {
	for {
		height, err := client.BlockNumber(ctx)
		if err != nil {
			return err
		}
		if height < n {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		break
	}

	return nil
}

func WaitReceiptOK(ctx context.Context, client *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	return WaitReceipt(ctx, client, hash, types.ReceiptStatusSuccessful)
}

func WaitReceiptFail(ctx context.Context, client *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	return WaitReceipt(ctx, client, hash, types.ReceiptStatusFailed)
}

func WaitReceipt(ctx context.Context, client *ethclient.Client, hash common.Hash, status uint64) (*types.Receipt, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		receipt, err := client.TransactionReceipt(ctx, hash)
		if errors.Is(err, ethereum.NotFound) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		if receipt.Status != status {
			return receipt, fmt.Errorf("expected status %d, but got %d", status, receipt.Status)
		}
		return receipt, nil
	}
}
