// Copyright 2018 TiKV Project Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package cases

import (
	"bytes"
	"fmt"
	"math/rand"

	"go.uber.org/zap"

	"github.com/pingcap/kvproto/pkg/metapb"
	"github.com/tikv/pd/pkg/codec"
	"github.com/tikv/pd/server/core"
	"github.com/tikv/pd/tools/pd-simulator/simulator/info"
	"github.com/tikv/pd/tools/pd-simulator/simulator/simutil"
)

func newImportData() *Case {
	var simCase Case
	// Initialize the cluster
	for i := 1; i <= 10; i++ {
		simCase.Stores = append(simCase.Stores, &Store{
			ID:        IDAllocator.nextID(),
			Status:    metapb.StoreState_Up,
			Capacity:  1 * TB,
			Available: 900 * GB,
			Version:   "2.1.0",
		})
	}

	for i := 0; i < getRegionNum(); i++ {
		storeIDs := rand.Perm(10)
		peers := []*metapb.Peer{
			{Id: IDAllocator.nextID(), StoreId: uint64(storeIDs[0] + 1)},
			{Id: IDAllocator.nextID(), StoreId: uint64(storeIDs[1] + 1)},
			{Id: IDAllocator.nextID(), StoreId: uint64(storeIDs[2] + 1)},
		}
		simCase.Regions = append(simCase.Regions, Region{
			ID:     IDAllocator.nextID(),
			Peers:  peers,
			Leader: peers[0],
			Size:   32 * MB,
			Keys:   320000,
		})
	}

	simCase.RegionSplitSize = 64 * MB
	simCase.RegionSplitKeys = 640000
	simCase.TableNumber = 10
	// Events description
	e := &WriteFlowOnSpotDescriptor{}
	table12 := string(codec.EncodeBytes(codec.GenerateTableKey(12)))
	table13 := string(codec.EncodeBytes(codec.GenerateTableKey(13)))
	e.Step = func(tick int64) map[string]int64 {
		if tick > int64(getRegionNum())/10 {
			return nil
		}
		return map[string]int64{
			table12: 32 * MB,
		}
	}
	simCase.Events = []EventDescriptor{e}

	// Checker description
	checkCount := uint64(0)
	simCase.Checker = func(regions *core.RegionsInfo, stats []info.StoreStats) bool {
		leaderDist := make(map[uint64]int)
		peerDist := make(map[uint64]int)
		leaderTotal := 0
		peerTotal := 0
		res := make([]*core.RegionInfo, 0, 100)
		regions.ScanRangeWithIterator([]byte(table12), func(region *core.RegionInfo) bool {
			if bytes.Compare(region.GetEndKey(), []byte(table13)) < 0 {
				res = append(res, regions.GetRegion(region.GetID()))
				return true
			}
			return false
		})

		for _, r := range res {
			leaderTotal++
			leaderDist[r.GetLeader().GetStoreId()]++
			for _, p := range r.GetPeers() {
				peerDist[p.GetStoreId()]++
				peerTotal++
			}
		}
		if leaderTotal == 0 || peerTotal == 0 {
			return false
		}
		tableLeaderLog := fmt.Sprintf("%d leader:", leaderTotal)
		tablePeerLog := fmt.Sprintf("%d peer: ", peerTotal)
		for storeID := 1; storeID <= 10; storeID++ {
			if leaderCount, ok := leaderDist[uint64(storeID)]; ok {
				tableLeaderLog = fmt.Sprintf("%s [store %d]:%.2f%%", tableLeaderLog, storeID, float64(leaderCount)/float64(leaderTotal)*100)
			}
		}
		for storeID := 1; storeID <= 10; storeID++ {
			if peerCount, ok := peerDist[uint64(storeID)]; ok {
				tablePeerLog = fmt.Sprintf("%s [store %d]:%.2f%%", tablePeerLog, storeID, float64(peerCount)/float64(peerTotal)*100)
			}
		}
		regionTotal := regions.GetRegionCount()
		totalLeaderLog := fmt.Sprintf("%d leader:", regionTotal)
		totalPeerLog := fmt.Sprintf("%d peer:", regionTotal*3)
		isEnd := false
		var regionProps []float64
		for storeID := uint64(1); storeID <= 10; storeID++ {
			regions.GetStoreRegionCount(storeID)
			totalLeaderLog = fmt.Sprintf("%s [store %d]:%.2f%%", totalLeaderLog, storeID, float64(regions.GetStoreLeaderCount(storeID))/float64(regionTotal)*100)
			regionProp := float64(regions.GetStoreRegionCount(storeID)) / float64(regionTotal*3) * 100
			regionProps = append(regionProps, regionProp)
			totalPeerLog = fmt.Sprintf("%s [store %d]:%.2f%%", totalPeerLog, storeID, regionProp)
		}
		simutil.Logger.Info("import data information",
			zap.String("table-leader", tableLeaderLog),
			zap.String("table-peer", tablePeerLog),
			zap.String("total-leader", totalLeaderLog),
			zap.String("total-peer", totalPeerLog))
		checkCount++
		dev := 0.0
		for _, p := range regionProps {
			dev += (p - 10) * (p - 10) / 100
		}
		if dev > 0.005 {
			simutil.Logger.Warn("Not balanced, change scheduler or store limit", zap.Float64("dev score", dev))
		}
		if checkCount > uint64(getRegionNum())/10 {
			isEnd = dev < 0.002
		}
		return isEnd
	}
	return &simCase
}
