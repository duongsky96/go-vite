package consensus

import (
	"time"

	"github.com/vitelabs/go-vite/consensus/db"

	"github.com/vitelabs/go-vite/ledger"

	"github.com/hashicorp/golang-lru"
	"github.com/vitelabs/go-vite/common/types"
)

func newDayLinkedArray(hour *linkedArray, db *consensus_db.ConsensusDB) *linkedArray {
	day := &linkedArray{}
	day.rate = DAY_TO_HOUR
	day.prefix = consensus_db.INDEX_Point_DAY
	day.lowerArr = hour
	day.db = db
	return day
}

func newHourLinkedArray(period *periodLinkedArray, db *consensus_db.ConsensusDB) *linkedArray {
	hourArr := &linkedArray{}
	hourArr.rate = HOUR_TO_PERIOD
	hourArr.prefix = consensus_db.INDEX_Point_HOUR
	hourArr.lowerArr = period
	hourArr.db = db
	return hourArr
}

func newPeriodLinkedArray() {

}

type LinkedArray interface {
	GetByHeight(height uint64) (*consensus_db.Point, error)
}

type linkedArray struct {
	prefix   byte
	rate     uint64
	db       *consensus_db.ConsensusDB
	lowerArr LinkedArray
}

func (self *linkedArray) GetByHeight(height uint64) (*consensus_db.Point, error) {
	point, err := self.db.GetPointByHeight(self.prefix, height)
	if err != nil {
		return nil, err
	}
	if point != nil {
		return point, nil
	}

	return self.getByHeight(height)
}

func (self *linkedArray) getByHeight(height uint64) (*consensus_db.Point, error) {
	result := &consensus_db.Point{}
	start := height * self.rate
	end := start + self.rate
	for i := start; i < end; i++ {
		p, err := self.lowerArr.GetByHeight(i)
		if err != nil {
			return nil, err
		}

		if err := result.Append(p); err != nil {
			return nil, err
		}
	}
	return result, nil
}

var HOUR_TO_PERIOD = uint64(48)
var DAY_TO_HOUR = uint64(24)
var DAY_TO_PERIOD = uint64(24 * 48)

//// hour = 48 * period
//type hourPoint struct {
//	hashPoint
//}

type SBPInfo struct {
	ExpectedNum int32
	FactualNum  int32
}

// period = 75s
type periodPoint struct {
	*consensus_db.Point
	empty bool
	// hash exist
	proof *ledger.HashHeight
	// beforeTime + hash
	proof2 *ledger.HashHeight
	stime  *time.Time
	etime  *time.Time
}

type periodLinkedArray struct {
	//periods map[uint64]*periodPoint
	periods  *lru.Cache
	rw       ch
	snapshot *snapshotCs
}

func newPeriodPointArray(rw ch, cs *snapshotCs) *periodLinkedArray {
	cache, err := lru.New(4 * 24 * 60)
	if err != nil {
		panic(err)
	}
	return &periodLinkedArray{rw: rw, periods: cache, snapshot: cs}
}

func (self *periodLinkedArray) GetByHeight(height uint64) (*consensus_db.Point, error) {
	value, ok := self.periods.Get(height)
	if !ok || value == nil {
		result, err := self.getByHeight(height)
		if err != nil {
			return nil, err
		}
		if result != nil {
			self.Set(height, result)
			return result.Point, nil
		} else {
			return nil, nil
		}
	}
	point := value.(*periodPoint)
	valid := self.checkValid(point)
	if !valid {
		result, err := self.getByHeight(height)
		if err != nil {
			return nil, err
		}
		if result != nil {
			self.Set(height, result)
			return result.Point, nil
		} else {
			return nil, nil
		}
	}
	return point.Point, nil
}

func (self *periodLinkedArray) Set(height uint64, block *periodPoint) error {
	self.periods.Add(height, block)
	return nil
}

func (self *periodLinkedArray) NextHeight(height uint64) uint64 {
	return height + 1
}

func (self *periodLinkedArray) getByHeight(height uint64) (*periodPoint, error) {
	stime, etime := self.snapshot.index2Time(height)
	// todo opt
	endSnapshotBlock, err := self.rw.GetSnapshotHeaderBeforeTime(&etime)
	if err != nil {
		return nil, err
	}
	if endSnapshotBlock.Timestamp.Before(stime) {
		return self.emptyPoint(height, &stime, &etime, endSnapshotBlock)
	}

	if self.rw.IsGenesisSnapshotBlock(endSnapshotBlock.Hash) {
		return self.emptyPoint(height, &stime, &etime, endSnapshotBlock)
	}

	blocks, err := self.rw.GetSnapshotHeadersAfterOrEqualTime(&ledger.HashHeight{Hash: endSnapshotBlock.Hash, Height: endSnapshotBlock.Height}, &stime, nil)
	if err != nil {
		return nil, err
	}

	// actually no block
	if len(blocks) == 0 {
		return self.emptyPoint(height, &stime, &etime, endSnapshotBlock)
	}

	result, err := self.snapshot.electionIndex(height)
	if err != nil {
		return nil, err
	}

	return self.genPeriodPoint(height, &stime, &etime, endSnapshotBlock, blocks, result)
}

func (self *periodLinkedArray) checkValid(point *periodPoint) bool {
	proof := point.proof
	if proof != nil {
		block, _ := self.rw.GetSnapshotBlockByHash(proof.Hash)
		if block == nil {
			return false
		} else {
			return true
		}
	}

	proof2 := point.proof2
	if proof2 != nil {
		if point.etime == nil {
			panic("etime is nil")
		}
		block, _ := self.rw.GetSnapshotHeaderBeforeTime(point.etime)
		if block != nil && block.Hash == proof2.Hash {
			return true
		} else {
			return false
		}
	}
	return false
}

func (self *periodLinkedArray) emptyPoint(height uint64, stime, etime *time.Time, endSnapshotBlock *ledger.SnapshotBlock) (*periodPoint, error) {
	point := &periodPoint{}
	point.stime = stime
	point.etime = etime
	point.empty = true

	block, err := self.rw.GetSnapshotBlockByHeight(endSnapshotBlock.Height + 1)
	if err != nil {
		return nil, err
	}
	if block != nil && block.Timestamp.After(*etime) {
		point.proof = &ledger.HashHeight{Hash: block.Hash, Height: block.Height}
	} else {
		point.proof2 = &ledger.HashHeight{Hash: endSnapshotBlock.Hash, Height: endSnapshotBlock.Height}
	}
	return point, nil
}
func (self *periodLinkedArray) genPeriodPoint(height uint64, stime *time.Time, etime *time.Time, endSnapshot *ledger.SnapshotBlock, blocks []*ledger.SnapshotBlock, result *electionResult) (*periodPoint, error) {
	point := &periodPoint{}
	point.stime = stime
	point.etime = etime
	point.empty = false

	block, err := self.rw.GetSnapshotBlockByHeight(endSnapshot.Height + 1)
	if err != nil {
		return nil, err
	}
	if block != nil && (block.Timestamp.Nanosecond() >= etime.Nanosecond()) {
		point.proof = &ledger.HashHeight{Hash: block.Hash, Height: block.Height}
		point.Hash = &block.Hash
	} else {
		point.proof2 = &ledger.HashHeight{Hash: endSnapshot.Hash, Height: endSnapshot.Height}
	}
	point.PrevHash = &blocks[len(blocks)-1].Hash

	sbps := make(map[types.Address]*consensus_db.Content)
	for _, v := range blocks {
		sbp, ok := sbps[v.Producer()]
		if !ok {
			sbps[v.Producer()] = &consensus_db.Content{FactualNum: 1, ExpectedNum: 0}
		} else {
			sbp.AddNum(0, 1)
		}
	}

	for _, v := range result.Plans {
		sbp, ok := sbps[v.Member]
		if !ok {
			sbps[v.Member] = &consensus_db.Content{FactualNum: 0, ExpectedNum: 1}
		} else {
			sbp.AddNum(1, 0)
		}
	}
	point.Sbps = sbps
	return point, nil
}