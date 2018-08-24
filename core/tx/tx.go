package tx

import (
	"fmt"
	"time"

	"bytes"
	"errors"
	"strconv"

	"github.com/gogo/protobuf/proto"
	"github.com/iost-official/Go-IOS-Protocol/account"
	"github.com/iost-official/Go-IOS-Protocol/common"
	"github.com/iost-official/Go-IOS-Protocol/crypto"
)

//go:generate protoc  --go_out=plugins=grpc:. ./core/tx/tx.proto

// Tx Transaction structure
type Tx struct {
	hash       []byte             `json:"-"`
	Time       int64              `json:"time,string"`
	Expiration int64              `json:"expiration,string"`
	GasLimit   int64              `json:"gas_limit,string"`
	Actions    []Action           `json:"-"`
	Signers    [][]byte           `json:"-"`
	Signs      []common.Signature `json:"-"`
	Publisher  common.Signature   `json:"-"`
	GasPrice   int64              `json:"gas_price,string"`
}

// NewTx return a new Tx
func NewTx(actions []Action, signers [][]byte, gasLimit int64, gasPrice int64, expiration int64) Tx {
	now := time.Now().UnixNano()
	return Tx{
		Time:       now,
		Actions:    actions,
		Signers:    signers,
		GasLimit:   gasLimit,
		GasPrice:   gasPrice,
		Expiration: expiration,
		hash:       nil,
	}
}

// SignTxContent sign tx content, only signers should do this
func SignTxContent(tx Tx, account account.Account) (common.Signature, error) {
	if !tx.containSigner(account.Pubkey) {
		return common.Signature{}, errors.New("account not included in signer list of this transaction")
	}
	return common.Sign(crypto.Secp256k1, tx.baseHash(), account.Seckey, common.SavePubkey), nil
}

func (t *Tx) containSigner(pubkey []byte) bool {
	found := false
	for _, signer := range t.Signers {
		if bytes.Equal(signer, pubkey) {
			found = true
		}
	}
	return found
}

func (t *Tx) baseHash() []byte {
	tr := &TxRaw{
		Time:       t.Time,
		Expiration: t.Expiration,
		GasLimit:   t.GasLimit,
		GasPrice:   t.GasPrice,
	}
	for _, a := range t.Actions {
		tr.Actions = append(tr.Actions, &ActionRaw{
			Contract:   a.Contract,
			ActionName: a.ActionName,
			Data:       a.Data,
		})
	}
	tr.Signers = t.Signers

	b, err := proto.Marshal(tr)
	if err != nil {
		panic(err)
	}
	return common.Sha256(b)
}

// SignTx sign the whole tx, including signers' signature, only publisher should do this
func SignTx(tx Tx, account account.Account, signs ...common.Signature) (Tx, error) {
	tx.Signs = append(tx.Signs, signs...)
	tx.Publisher = common.Sign(crypto.Secp256k1, tx.publishHash(), account.Seckey, common.SavePubkey)
	return tx, nil
}

// publishHash
func (t *Tx) publishHash() []byte {
	tr := &TxRaw{
		Time:       t.Time,
		Expiration: t.Expiration,
		GasLimit:   t.GasLimit,
		GasPrice:   t.GasPrice,
	}
	for _, a := range t.Actions {
		tr.Actions = append(tr.Actions, &ActionRaw{
			Contract:   a.Contract,
			ActionName: a.ActionName,
			Data:       a.Data,
		})
	}
	tr.Signers = t.Signers
	for _, s := range t.Signs {
		tr.Signs = append(tr.Signs, &common.SignatureRaw{
			Algorithm: int32(s.Algorithm),
			Sig:       s.Sig,
			PubKey:    s.Pubkey,
		})
	}

	b, err := proto.Marshal(tr)
	if err != nil {
		panic(err)
	}
	return common.Sha256(b)
}

// ToTxRaw convert tx to TxRaw for transmission
func (t *Tx) ToTxRaw() *TxRaw {
	tr := &TxRaw{
		Time:       t.Time,
		Expiration: t.Expiration,
		GasLimit:   t.GasLimit,
		GasPrice:   t.GasPrice,
	}
	for _, a := range t.Actions {
		tr.Actions = append(tr.Actions, &ActionRaw{
			Contract:   a.Contract,
			ActionName: a.ActionName,
			Data:       a.Data,
		})
	}
	tr.Signers = t.Signers
	for _, s := range t.Signs {
		tr.Signs = append(tr.Signs, &common.SignatureRaw{
			Algorithm: int32(s.Algorithm),
			Sig:       s.Sig,
			PubKey:    s.Pubkey,
		})
	}
	tr.Publisher = &common.SignatureRaw{
		Algorithm: int32(t.Publisher.Algorithm),
		Sig:       t.Publisher.Sig,
		PubKey:    t.Publisher.Pubkey,
	}
	return tr
}

// Encode tx to byte array
func (t *Tx) Encode() []byte {
	tr := t.ToTxRaw()
	b, err := proto.Marshal(tr)
	if err != nil {
		panic(err)
	}
	return b
}

// FromTxRaw convert tx from TxRaw
func (t *Tx) FromTxRaw(tr *TxRaw) {
	t.Time = tr.Time
	t.Expiration = tr.Expiration
	t.GasLimit = tr.GasLimit
	t.GasPrice = tr.GasPrice
	t.Actions = []Action{}
	for _, a := range tr.Actions {
		t.Actions = append(t.Actions, Action{
			Contract:   a.Contract,
			ActionName: a.ActionName,
			Data:       a.Data,
		})
	}
	t.Signers = tr.Signers
	t.Signs = []common.Signature{}
	for _, sr := range tr.Signs {
		t.Signs = append(t.Signs, common.Signature{
			Algorithm: crypto.Algorithm(sr.Algorithm),
			Sig:       sr.Sig,
			Pubkey:    sr.PubKey,
		})
	}
	t.Publisher = common.Signature{
		Algorithm: crypto.Algorithm(tr.Publisher.Algorithm),
		Sig:       tr.Publisher.Sig,
		Pubkey:    tr.Publisher.PubKey,
	}
	t.hash = nil
}

// Decode tx from byte array
func (t *Tx) Decode(b []byte) error {
	tr := &TxRaw{}
	err := proto.Unmarshal(b, tr)
	if err != nil {
		return err
	}
	t.FromTxRaw(tr)
	return nil
}

// String return human-readable tx
func (t *Tx) String() string {
	str := "Tx{\n"
	str += "	Time: " + strconv.FormatInt(t.Time, 10) + ",\n"
	str += "	Pubkey: " + string(t.Publisher.Pubkey) + ",\n"
	str += "	Action:\n"
	for _, a := range t.Actions {
		str += "		" + a.String()
	}
	str += "}\n"
	return str
}

// Hash return cached hash if exists, or calculate with Sha256
func (t *Tx) Hash() []byte {
	if t.hash == nil {
		t.hash = common.Sha256(t.Encode())
	}
	return t.hash
}

// VerifySelf verify tx's signature
func (t *Tx) VerifySelf() error {
	baseHash := t.baseHash()
	signerSet := make(map[string]bool)
	for _, sign := range t.Signs {
		ok := common.VerifySignature(baseHash, sign)
		if !ok {
			return fmt.Errorf("signer error")
		}
		signerSet[common.Base58Encode(sign.Pubkey)] = true
	}
	for _, signer := range t.Signers {
		if _, ok := signerSet[common.Base58Encode(signer)]; !ok {
			return fmt.Errorf("signer not enough")
		}
	}

	ok := common.VerifySignature(t.publishHash(), t.Publisher)
	if !ok {
		return fmt.Errorf("publisher error")
	}
	return nil
}

// VerifySelf verify signer's signature
func (t *Tx) VerifySigner(sig common.Signature) bool {
	return common.VerifySignature(t.baseHash(), sig)
}
