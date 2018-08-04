package pob

import (
	. "github.com/iost-official/Go-IOS-Protocol/account"
	. "github.com/iost-official/Go-IOS-Protocol/network"
	. "github.com/iost-official/Go-IOS-Protocol/new_consensus/common"

	"fmt"
	"time"

	"github.com/iost-official/Go-IOS-Protocol/core/message"
	"github.com/iost-official/Go-IOS-Protocol/core/new_block"
	"github.com/iost-official/Go-IOS-Protocol/core/new_blockcache"

	"github.com/iost-official/Go-IOS-Protocol/core/global"
	"github.com/iost-official/Go-IOS-Protocol/core/new_txpool"
	"github.com/iost-official/Go-IOS-Protocol/db"
	"github.com/iost-official/Go-IOS-Protocol/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	generatedBlockCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "generated_block_count",
			Help: "Count of generated block by current node",
		},
	)
	receivedBlockCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "received_block_count",
			Help: "Count of received block by current node",
		},
	)
	confirmedBlockchainLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "confirmed_blockchain_length",
			Help: "Length of confirmed blockchain on current node",
		},
	)
	txPoolSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "tx_poo_size",
			Help: "size of tx pool on current node",
		},
	)
)

func init() {
	prometheus.MustRegister(generatedBlockCount)
	prometheus.MustRegister(receivedBlockCount)
	prometheus.MustRegister(confirmedBlockchainLength)
	prometheus.MustRegister(txPoolSize)
}

type PoB struct {
	account      Account
	global       global.Global
	blockChain   block.Chain
	blockCache   *blockcache.BlockCache
	txPool       new_txpool.TxPool
	router       Router
	synchronizer Synchronizer
	verifyDB     *db.MVCCDB
	produceDB    *db.MVCCDB

	exitSignal chan struct{}
	chBlock    chan message.Message

	log *log.Logger
}

func NewPoB(acc Account, global global.Global, witnessList []string) (*PoB, error) {
	//TODO: change initialization based on new interfaces
	p := PoB{
		account:    acc,
		global:     global,
		blockCache: blockcache.NewBlockCache(),
		blockChain: global.BlockChain(),
		verifyDB:   global.StdPool(),
		txPool:     global.TxDB(),
	}

	p.produceDB = p.verifyDB.Fork()
	if p.blockChain.GetBlockByNumber(0) == nil {

		t := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		genesis, err := genGenesis(GetTimestamp(t.Unix()).Slot)
		if err != nil {
			return nil, fmt.Errorf("failed to genesis is nil")
		}
		//TODO: add genesis to db, what about its state?
		p.blockChain.Push(genesis)
	}

	// TODO: how to initialize network?
	var err error
	p.router = Route
	if p.router == nil {
		return nil, fmt.Errorf("failed to network.Route is nil")
	}

	p.synchronizer = NewSynchronizer(p.blockCache, p.router, len(witnessList)*2/3)
	if p.synchronizer == nil {
		return nil, err
	}

	p.chBlock, err = p.router.FilteredChan(Filter{
		AcceptType: []ReqType{ReqNewBlock, ReqSyncBlock}})
	if err != nil {
		return nil, err
	}
	p.exitSignal = make(chan struct{})

	p.log, err = log.NewLogger("consensus.log")
	if err != nil {
		return nil, err
	}

	p.log.NeedPrint = false

	p.initGlobalProperty(p.account, witnessList)

	dynamicProp.update(&p.blockChain.Top().Head)
	return &p, nil
}

func (p *PoB) initGlobalProperty(acc Account, witnessList []string) {
	staticProp = newGlobalStaticProperty(acc, witnessList)
	dynamicProp = newGlobalDynamicProperty()
}

func (p *PoB) Run() {
	p.synchronizer.StartListen()
	go p.blockLoop()
	go p.scheduleLoop()
}

func (p *PoB) Stop() {
	close(p.chBlock)
	close(p.exitSignal)
}

func (p *PoB) blockLoop() {
	p.log.I("Start to listen block")
	for {
		select {
		case req, ok := <-p.chBlock:
			if !ok {
				return
			}
			var blk block.Block
			err := blk.Decode(req.Body)
			if err != nil {
				continue
			}
			parent := p.blockCache.Find(blk.HeadHash())
			if err := verifyBasics(blk, parent); err == nil {
				// tell synchronizer to cancel downloading

				if parent.Type == blockcache.Linked {
					var node *blockcache.BlockCacheNode
					err := p.addBlock(&blk, node, parent, true)
					if err != nil {
						// dishonest?
						continue
					}
					p.addSingles(node)
				}
			} else {
				// dishonest?
			}
		case <-p.exitSignal:
			return
		}
	}
}

func (p *PoB) scheduleLoop() {
	var nextSchedule int64
	nextSchedule = 0
	p.log.I("Start to schedule")
	for {
		select {
		case <-p.exitSignal:
			return
		case <-time.After(time.Second * time.Duration(nextSchedule)):
			currentTimestamp := GetCurrentTimestamp()
			wid := witnessOfTime(currentTimestamp)
			p.log.I("currentTimestamp: %v, wid: %v, p.account.ID: %v", currentTimestamp, wid, p.account.ID)
			if wid == p.account.ID && p.global.Mode() == global.ModeNormal {
				chainHead := p.blockCache.Head
				p.produceDB.Checkout(chainHead.Block.HeadHash())
				blk := genBlock(p.account, chainHead, p.produceDB)

				dynamicProp.update(&blk.Head)
				p.log.I("Generating block, current timestamp: %v number: %v", currentTimestamp, blk.Head.Number)

				bb := blk.Encode()
				msg := message.Message{ReqType: int32(ReqNewBlock), Body: bb}
				log.Log.I("Block size: %v, TrNum: %v", len(bb), len(blk.Txs))
				go p.router.Broadcast(msg)
				p.chBlock <- msg
				p.log.I("Broadcasted block, current timestamp: %v number: %v", currentTimestamp, blk.Head.Number)
			}
			nextSchedule = timeUntilNextSchedule(time.Now().Unix())
		}
	}
}

func (p *PoB) addBlock(blk *block.Block, node *blockcache.BlockCacheNode, parent *blockcache.BlockCacheNode, newBlock bool) error {
	// verify block txs
	if blk.Head.Witness != p.account.ID {
		p.verifyDB.Checkout(parent.Block.HeadHash())
		err := verifyBlockTxs(blk, p.verifyDB)
		// add
		if newBlock {
			if err == nil {
				node, err = p.blockCache.Add(blk)
			} else {
				return err
			}
		} else {
			if err != nil {
				p.blockCache.Del(node)
				return err
			}
		}
		// tag in state
		p.verifyDB.Tag(blk.HeadHash())
	} else {
		p.verifyDB.Checkout(blk.HeadHash())
	}
	// update node info without state
	updateNodeInfo(node)
	// update node info with state, currently pending witness list
	updatePendingWitness(node, p.verifyDB)

	// confirm
	confirmNode := calculateConfirm(node, p.blockCache.Head)
	if confirmNode != nil {
		p.blockCache.Flush(confirmNode)
	}

	// promote witness list
	promoteWitness(node, confirmNode)

	dynamicProp.update(&blk.Head)
	// -> tx pool
	isHead := (node == p.blockCache.Head)
	new_txpool.TxPoolS.AddConfirmBlock(blk, isHead)
}

func (p *PoB) addSingles(node *blockcache.BlockCacheNode) {
	if node.Children != nil {
		for i := range node.Children {
			p.addBlock(nil, node.Children[i], node, false)
			p.addSingles(node.Children[i])
		}
	}
}