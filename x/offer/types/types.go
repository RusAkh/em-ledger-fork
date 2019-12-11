package types

import (
	"fmt"
	"strings"

	"github.com/emirpasic/gods/trees/btree"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Instrument struct {
	Source, Destination string

	Orders *btree.Tree
}

type Instruments []Instrument

func (is Instruments) String() string {
	sb := strings.Builder{}

	for _, instr := range is {
		sb.WriteString(fmt.Sprintf("%v/%v - %v\n", instr.Source, instr.Destination, instr.Orders.Size()))
	}

	return sb.String()
}

func (is *Instruments) InsertOrder(order *Order) {
	for _, i := range *is {
		if i.Destination == order.Destination.Denom && i.Source == order.Source.Denom {
			i.Orders.Put(order, nil)
			return
		}
	}

	i := Instrument{
		Source:      order.Source.Denom,
		Destination: order.Destination.Denom,
		Orders:      btree.NewWith(3, OrderPriorityComparator),
	}

	*is = append(*is, i)
	i.Orders.Put(order, nil)
}

func (is *Instruments) GetInstrument(source, destination string) *Instrument {
	for _, i := range *is {
		if i.Source == source && i.Destination == destination {
			return &i
		}
	}

	return nil
}

func (is *Instruments) RemoveInstrument(instr Instrument) {
	for index, v := range *is {
		if instr.Source == v.Source && instr.Destination == v.Destination {
			*is = append((*is)[:index], (*is)[index+1:]...)
			return
		}
	}
}

type Order struct {
	ID uint64

	Source, Destination sdk.Coin
	SourceRemaining     sdk.Int

	Owner         sdk.AccAddress
	ClientOrderID string

	SourceAccount sdk.AccAddress

	price,
	invertedPrice float64
}

// Should return a number:
//    negative , if a < b
//    zero     , if a == b
//    positive , if a > b
func OrderPriorityComparator(a, b interface{}) int {
	aAsserted := a.(*Order)
	bAsserted := b.(*Order)

	// TODO Unit tests

	// Price priority
	switch {
	case aAsserted.Price() < bAsserted.Price():
		return -1
	case aAsserted.Price() > bAsserted.Price():
		return 1
	}

	// Time priority
	return int(aAsserted.ID - bAsserted.ID)
}

func (o Order) InvertedPrice() float64 {
	return o.invertedPrice
}

func (o Order) Price() float64 {
	return o.price
}

func (o Order) String() string {
	return fmt.Sprintf("%d : %v -> %v @ %v/%v (%v remaining)", o.ID, o.Source, o.Destination, o.price, o.invertedPrice, o.SourceRemaining)
}

func NewOrder(src, dst sdk.Coin, seller sdk.AccAddress, clientOrderId string) *Order {
	return &Order{
		SourceAccount:   seller,
		Source:          src,
		Destination:     dst,
		SourceRemaining: src.Amount,
		ClientOrderID:   clientOrderId,
		price:           float64(dst.Amount.Int64()) / float64(src.Amount.Int64()),
		invertedPrice:   float64(src.Amount.Int64()) / float64(dst.Amount.Int64()),
	}
}
