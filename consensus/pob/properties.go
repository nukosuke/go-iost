package pob

import (
	"github.com/iost-official/Go-IOS-Protocol/account"
	"github.com/iost-official/Go-IOS-Protocol/common"
)

var staticProperty *StaticProperty

// StaticProperty handles the the static property of pob.
type StaticProperty struct {
	account           *account.Account
	NumberOfWitnesses int64
	WitnessList       []string
	WitnessMap        map[string]int64
	Watermark         map[string]int64
}

func newStaticProperty(account *account.Account, witnessList []string) *StaticProperty {
	property := &StaticProperty{
		account:     account,
		WitnessList: make([]string, 0),
		WitnessMap:  make(map[string]int64),
		Watermark:   make(map[string]int64),
		SlotMap:     new(sync.Map),
	}

	property.updateWitness(witnessList)

	return property
}

func (property *StaticProperty) hasSlot(slot int64) bool {
	_, ok := property.SlotMap.Load(slot)
	return ok
}

func (property *StaticProperty) addSlot(slot int64) {
	property.SlotMap.Store(slot, true)
}

func (property *StaticProperty) delSlot(slot int64) {
	if slot%10 != 0 {
		return
	}
	property.SlotMap.Range(func(k, v interface{}) bool {
		s, sok := k.(int64)
		if !sok || s <= slot {
			property.SlotMap.Delete(k)
		}
		return true
	})
}

func (property *StaticProperty) updateWitness(witnessList []string) {

	property.NumberOfWitnesses = int64(len(witnessList))
	property.WitnessList = witnessList

	for k := range property.WitnessMap {
		delete(property.WitnessMap, k)
	}

	for i, w := range witnessList {
		property.WitnessMap[w] = int64(i)
	}
}

var (
	second2nanosecond int64 = 1000000000
)

func witnessOfSec(sec int64) string {
	return witnessOfSlot(sec / common.SlotLength)
}

func witnessOfSlot(slot int64) string {
	index := slot % staticProperty.NumberOfWitnesses
	witness := staticProperty.WitnessList[index]
	return witness
}

func timeUntilNextSchedule(timeSec int64) int64 {
	currentSlot := timeSec / (second2nanosecond * common.SlotLength)
	return (currentSlot+1)*second2nanosecond*common.SlotLength - timeSec
}
