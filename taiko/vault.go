package taiko

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/hive/hivesim"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	VaultAddr = common.HexToAddress("0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266")
	// This is the account that sends vault funding transactions.
	vaultKey, _ = crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	// Number of blocks to wait before funding tx is considered valid.
	vaultTxConfirmationCount = uint64(1)
)

// Vault creates accounts for testing and funds them. An instance of the Vault contract is
// deployed in the genesis block. When creating a new account using CreateAccount, the
// account is funded by sending a transaction to this contract.
//
// The purpose of the vault is allowing tests to run concurrently without worrying about
// nonce assignment and unexpected balance changes.
type Vault struct {
	t       *hivesim.T
	chainID *big.Int

	// This tracks the account nonce of the vault account.
	nonce uint64
	// Created accounts are tracked in this map.
	accounts map[common.Address]*ecdsa.PrivateKey

	mu sync.Mutex
}

func NewVault(t *hivesim.T, chainID *big.Int) *Vault {
	return &Vault{
		t:        t,
		chainID:  chainID,
		accounts: make(map[common.Address]*ecdsa.PrivateKey),
	}
}

// GenerateKey creates a new account key and stores it.
func (v *Vault) GenerateKey() common.Address {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic(fmt.Errorf("can'T generate account key: %v", err))
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.accounts[addr] = key
	return addr
}

// FindKey returns the private key for an address.
func (v *Vault) FindKey(addr common.Address) *ecdsa.PrivateKey {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.accounts[addr]
}

// SignTransaction signs the given transaction with the test account and returns it.
// It uses the EIP155 signing rules.
func (v *Vault) SignTransaction(sender common.Address, tx *types.Transaction) (*types.Transaction, error) {
	key := v.FindKey(sender)
	if key == nil {
		return nil, fmt.Errorf("sender account %v not in vault", sender)
	}
	signer := types.LatestSignerForChainID(v.chainID)
	return types.SignTx(tx, signer, key)
}

// CreateAccount creates a new account that is funded from the vault contract.
// It will panic when the account could not be created and funded.
func (v *Vault) CreateAccount(ctx context.Context, client *ethclient.Client, amount *big.Int) common.Address {
	if amount == nil {
		amount = new(big.Int)
	}
	address := v.GenerateKey()

	// order the vault to send some ether
	tx := v.makeFundingTx(address, amount)
	if err := client.SendTransaction(ctx, tx); err != nil {
		v.t.Fatalf("unable to send funding transaction: %v", err)
	}

	for i := 0; i < 60; i++ {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err != nil && !errors.Is(err, ethereum.NotFound) {
			v.t.Fatal("error getting transaction receipt:", err)
		}
		if receipt != nil {
			return address
		}
		time.Sleep(time.Second)
	}

	v.t.Fatal("timed out getting transaction receipt")
	return common.Address{}
}

func (v *Vault) InsertKey(key *ecdsa.PrivateKey) {
	addr := crypto.PubkeyToAddress(key.PublicKey)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.accounts[addr] = key
}

func (v *Vault) KeyedTransactor(addr common.Address) *bind.TransactOpts {
	opts, err := bind.NewKeyedTransactorWithChainID(v.FindKey(addr), v.chainID)
	if err != nil {
		v.t.Fatal("error getting keyed transactor:", err)
	}
	return opts
}

func (v *Vault) makeFundingTx(recipient common.Address, amount *big.Int) *types.Transaction {
	var (
		nonce    = v.nextNonce()
		gasLimit = uint64(75000)
	)
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		Gas:       gasLimit,
		GasTipCap: big.NewInt(1 * params.GWei),
		GasFeeCap: gasPrice,
		To:        &recipient,
		Value:     amount,
	})
	signer := types.LatestSignerForChainID(v.chainID)
	signedTx, err := types.SignTx(tx, signer, vaultKey)
	if err != nil {
		v.t.Fatal("can'T sign vault funding tx:", err)
	}
	return signedTx
}

// nextNonce generates the nonce of a funding transaction.
func (v *Vault) nextNonce() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	nonce := v.nonce
	v.nonce++
	return nonce
}
