package chain_genesis

import (
	"time"

	"github.com/vitelabs/go-vite/interfaces"
	ledger "github.com/vitelabs/go-vite/interfaces/core"
)

func newGenesisSnapshotContent(accountBlocks []*interfaces.VmAccountBlock) ledger.SnapshotContent {
	sc := make(ledger.SnapshotContent, len(accountBlocks))
	for _, vmBlock := range accountBlocks {
		accountBlock := vmBlock.AccountBlock
		if hashHeight, ok := sc[accountBlock.AccountAddress]; !ok || hashHeight.Height < accountBlock.Height {
			sc[accountBlock.AccountAddress] = &ledger.HashHeight{
				Height: accountBlock.Height,
				Hash:   accountBlock.Hash,
			}
		}
	}

	return sc
}

func NewGenesisSnapshotBlock(accountBlocks []*interfaces.VmAccountBlock) *ledger.SnapshotBlock {
	// 2019/05/21 12:00:00 UTC/GMT +8
	genesisTimestamp := time.Unix(1558411200, 0)

	genesisSnapshotBlock := &ledger.SnapshotBlock{
		Height:          1,                 // height
		Timestamp:       &genesisTimestamp, // timestamp
		SnapshotContent: newGenesisSnapshotContent(accountBlocks),
	}

	genesisSnapshotBlock.Hash = genesisSnapshotBlock.ComputeHash()

	return genesisSnapshotBlock
}
