// This software is Copyright (c) 2019-2020 e-Money A/S. It is not offered under an open source license.
//
// Please contact partners@e-money.com for licensing related questions.

package keeper

import (
	"fmt"
	"math"
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authe "github.com/cosmos/cosmos-sdk/x/auth/exported"

	"github.com/e-money/em-ledger/x/market/types"
)

type Keeper struct {
	key         sdk.StoreKey
	cdc         *codec.Codec
	instruments types.Instruments
	ak          types.AccountKeeper
	bk          types.BankKeeper
	sk          types.SupplyKeeper

	accountOrders types.Orders
	appstateInit  *sync.Once
}

func NewKeeper(cdc *codec.Codec, key sdk.StoreKey, authKeeper types.AccountKeeper, bankKeeper types.BankKeeper, supplyKeeper types.SupplyKeeper) *Keeper {
	k := &Keeper{
		cdc: cdc,
		key: key,
		ak:  authKeeper,
		bk:  bankKeeper,
		sk:  supplyKeeper,

		accountOrders: types.NewOrders(),
		appstateInit:  new(sync.Once),
	}

	authKeeper.AddAccountListener(k.accountChanged)
	return k
}

func (k *Keeper) createExecutionPlan(ctx sdk.Context, SourceDenom string, DestinationDenom string) types.ExecutionPlan {
	result := types.ExecutionPlan{
		Price: sdk.NewDec(math.MaxInt64),
	}

	// Find the best direct or synthetic price for a given source/destination denom
	for _, firstInstrument := range k.instruments {
		if firstInstrument.Source == SourceDenom {
			if firstInstrument.Orders.Empty() {
				continue
			}

			firstPassiveOrder := firstInstrument.Orders.LeftKey().(*types.Order)

			// Check direct price
			if firstInstrument.Destination == DestinationDenom {
				// Direct price is better than current plan

				planPrice := sdk.OneDec().Quo(firstPassiveOrder.Price())
				if planPrice.LT(result.Price) {
					result = types.ExecutionPlan{
						Price:      planPrice,
						FirstOrder: firstPassiveOrder,
					}
				}
			}

			// Check synthetic price by going through two orders:
			// (SourceDenom, X) -> (X, DestinationDenom)
			for _, secondInstrument := range k.instruments {
				if secondInstrument.Source != firstInstrument.Destination {
					continue
				}

				if secondInstrument.Destination != DestinationDenom {
					continue
				}

				if secondInstrument.Orders.Empty() {
					continue
				}

				secondPassiveOrder := secondInstrument.Orders.LeftKey().(*types.Order)

				planPrice := sdk.OneDec().Quo(firstPassiveOrder.Price().Mul(secondPassiveOrder.Price()))

				if planPrice.LT(result.Price) {
					result = types.ExecutionPlan{
						Price:       planPrice,
						FirstOrder:  firstPassiveOrder,
						SecondOrder: secondPassiveOrder,
					}
				}
			}
		}
	}

	return result
}

func (k *Keeper) NewOrderSingle(ctx sdk.Context, aggressiveOrder types.Order) sdk.Result {
	if err := aggressiveOrder.IsValid(); err != nil {
		return err.Result()
	}

	if aggressiveOrder.IsFilled() {
		return types.ErrInvalidPrice(aggressiveOrder.Source, aggressiveOrder.Destination).Result()
	}

	sourceAccount := k.ak.GetAccount(ctx, aggressiveOrder.Owner)
	if sourceAccount == nil {
		return sdk.ErrUnknownAddress(fmt.Sprintf("account %s does not exist", aggressiveOrder.Owner.String())).Result()
	}

	// Verify account balance
	if _, anyNegative := sourceAccount.SpendableCoins(ctx.BlockTime()).SafeSub(sdk.NewCoins(aggressiveOrder.Source)); anyNegative {
		return types.ErrAccountBalanceInsufficient(aggressiveOrder.Owner, aggressiveOrder.Source, sourceAccount.SpendableCoins(ctx.BlockTime()).AmountOf(aggressiveOrder.Source.Denom)).Result()
	}

	// Verify uniqueness of client order id among active orders
	if k.accountOrders.ContainsClientOrderId(aggressiveOrder.Owner, aggressiveOrder.ClientOrderID) {
		return types.ErrNonUniqueClientOrderId(aggressiveOrder.Owner, aggressiveOrder.ClientOrderID).Result()
	}

	// Verify that the destination asset actually exists on chain before creating an instrument
	if !k.assetExists(ctx, aggressiveOrder.Destination) {
		return types.ErrUnknownAsset(aggressiveOrder.Destination).Result()
	}

	aggressiveOrder.ID = k.getNextOrderNumber(ctx)

	types.EmitNewOrderEvent(ctx, aggressiveOrder)

	for {
		plan := k.createExecutionPlan(ctx, aggressiveOrder.Destination.Denom, aggressiveOrder.Source.Denom)
		if plan.FirstOrder == nil {
			break
		}

		if aggressiveOrder.Price().GT(plan.Price) {
			// Spread has not been crossed. Aggressive order should be added to book.
			break
		}

		// All variables are named from the perspective of the passive order

		stepDestinationFilled := plan.DestinationCapacity()
		// Don't try to fill more than either the aggressive order capacity or the plan capacity (capacity of passive orders).
		stepDestinationFilled = sdk.MinDec(stepDestinationFilled, aggressiveOrder.SourceRemaining.ToDec())

		// Do not purchase more destination tokens than the order warrants
		aggressiveDestinationRemaining := aggressiveOrder.Destination.Amount.Sub(aggressiveOrder.DestinationFilled).ToDec().Quo(plan.Price)
		stepDestinationFilled = sdk.MinDec(stepDestinationFilled, aggressiveDestinationRemaining)

		for _, passiveOrder := range []*types.Order{plan.SecondOrder, plan.FirstOrder} {
			if passiveOrder == nil {
				continue
			}

			// Use the passive order's price in the market.
			stepSourceFilled := stepDestinationFilled.Quo(passiveOrder.Price())

			// Update the aggressive order during the plan's final step.
			if passiveOrder.Destination.Denom == aggressiveOrder.Source.Denom {
				aggressiveOrder.SourceRemaining = aggressiveOrder.SourceRemaining.Sub(stepDestinationFilled.RoundInt())
				aggressiveOrder.SourceFilled = aggressiveOrder.SourceFilled.Add(stepDestinationFilled.RoundInt())

				// Invariant check
				if aggressiveOrder.SourceRemaining.LT(sdk.ZeroInt()) {
					panic(fmt.Sprintf("Aggressive order's SourceRemaining field is less than zero. order: %v", aggressiveOrder))
				}
			}

			if passiveOrder.Source.Denom == aggressiveOrder.Destination.Denom {
				aggressiveOrder.DestinationFilled = aggressiveOrder.DestinationFilled.Add(stepSourceFilled.RoundInt())

				// Invariant check
				if aggressiveOrder.DestinationFilled.GT(aggressiveOrder.Destination.Amount) {
					panic(fmt.Sprintf("Aggressive order's DestinationFilled field is greater than Destination.Amount. order: %v", aggressiveOrder))
				}
			}

			passiveOrder.SourceRemaining = passiveOrder.SourceRemaining.Sub(stepSourceFilled.RoundInt())
			passiveOrder.SourceFilled = passiveOrder.SourceFilled.Add(stepSourceFilled.RoundInt())
			passiveOrder.DestinationFilled = passiveOrder.DestinationFilled.Add(stepDestinationFilled.RoundInt())

			// Invariant checks
			if passiveOrder.SourceRemaining.LT(sdk.ZeroInt()) {
				panic(fmt.Sprintf("Passive order's SourceRemaining field is less than zero. order: %v candidate: %v", aggressiveOrder, passiveOrder))
			}
			if passiveOrder.DestinationFilled.GT(passiveOrder.Destination.Amount) {
				panic(fmt.Sprintf("Passive order's DestinationFilled field is greater than Destination.Amount. order: %v", passiveOrder))
			}

			// Settle traded tokens
			nextDestinationFilledCoin := sdk.NewCoin(passiveOrder.Destination.Denom, stepDestinationFilled.RoundInt())
			nextSourceFilledCoin := sdk.NewCoin(passiveOrder.Source.Denom, stepSourceFilled.RoundInt())
			err := k.transferTradedAmounts(ctx, nextDestinationFilledCoin, nextSourceFilledCoin, passiveOrder.Owner, aggressiveOrder.Owner)
			if err != nil {
				panic(err)
			}

			if passiveOrder.IsFilled() {
				types.EmitFilledEvent(ctx, *passiveOrder)
				// Order has been filled. Remove it from queue.
				// Tx sender should not pay gas for completely filling passive orders.
				k.deleteOrder(ctx.WithGasMeter(sdk.NewInfiniteGasMeter()), passiveOrder)
			} else {
				types.EmitPartiallyFilledEvent(ctx, *passiveOrder)
				k.setOrder(ctx, passiveOrder)
			}

			stepDestinationFilled = stepSourceFilled
		}

		if aggressiveOrder.IsFilled() {
			types.EmitFilledEvent(ctx, aggressiveOrder)
			// Order has been filled.
			break
		}

		types.EmitPartiallyFilledEvent(ctx, aggressiveOrder)
	}

	if !aggressiveOrder.IsFilled() {
		// Order was not fully matched. Add to book.
		op := &aggressiveOrder
		k.instruments.InsertOrder(op)
		k.accountOrders.AddOrder(op)
		k.setOrder(ctx, op)
		// NOTE This should be the only place that an order is added to the book!
		// NOTE If this ceases to be true, move logic to func that cleans up all datastructures.
	}

	return sdk.Result{Events: ctx.EventManager().Events()}
}

func (k *Keeper) initializeFromStore(ctx sdk.Context) {
	// Loads the last known market state from app state.
	k.appstateInit.Do(func() {
		store := ctx.KVStore(k.key)
		it := store.Iterator(types.GetOrderKey(0), types.GetOrderKey(math.MaxUint64))
		for ; it.Valid(); it.Next() {
			o := &types.Order{}
			err := k.cdc.UnmarshalBinaryBare(it.Value(), o)
			if err != nil {
				panic(err)
			}

			k.instruments.InsertOrder(o)
			k.accountOrders.AddOrder(o)
		}
	})
}

// Check whether an asset even exists on the chain at the moment.
func (k Keeper) assetExists(ctx sdk.Context, asset sdk.Coin) bool {
	total := k.sk.GetSupply(ctx).GetTotal()
	return total.AmountOf(asset.Denom).GT(sdk.ZeroInt())
}

func (k *Keeper) CancelReplaceOrder(ctx sdk.Context, newOrder types.Order, origClientOrderId string) sdk.Result {
	origOrder := k.accountOrders.GetOrder(newOrder.Owner, origClientOrderId)
	if origOrder == nil {
		return types.ErrClientOrderIdNotFound(newOrder.Owner, origClientOrderId).Result()
	}

	// Verify that instrument is the same.
	if origOrder.Source.Denom != newOrder.Source.Denom || origOrder.Destination.Denom != newOrder.Destination.Denom {
		return types.ErrOrderInstrumentChanged(
			origOrder.Source.Denom, origOrder.Destination.Denom,
			newOrder.Source.Denom, newOrder.Destination.Denom,
		).Result()
	}

	resCancel := k.CancelOrder(ctx, newOrder.Owner, origClientOrderId)
	if !resCancel.IsOK() {
		return resCancel
	}

	// Adjust remaining according to how much of the replaced order was filled:
	newOrder.SourceFilled = origOrder.SourceFilled
	newOrder.SourceRemaining = newOrder.Source.Amount.Sub(newOrder.SourceFilled)
	newOrder.DestinationFilled = origOrder.DestinationFilled

	resAdd := k.NewOrderSingle(ctx, newOrder)
	if !resAdd.IsOK() {
		resAdd.Events = append(resAdd.Events, resCancel.Events...)
		return resAdd
	}

	evts := append(ctx.EventManager().Events(), resCancel.Events...)
	evts = append(evts, resAdd.Events...)
	return sdk.Result{Events: evts}
}

func (k *Keeper) CancelOrder(ctx sdk.Context, owner sdk.AccAddress, clientOrderId string) sdk.Result {
	orders := k.accountOrders.GetAllOrders(owner)

	var order *types.Order
	i, _ := orders.Find(func(index int, v interface{}) bool {
		order = v.(*types.Order)
		return order.ClientOrderID == clientOrderId
	})

	if i == -1 {
		return types.ErrClientOrderIdNotFound(owner, clientOrderId).Result()
	}

	k.deleteOrder(ctx, order)
	types.EmitCancelEvent(ctx, *order)

	return sdk.Result{Events: ctx.EventManager().Events()}
}

// Update any orders that can no longer be filled with the account's balance.
func (k *Keeper) accountChanged(ctx sdk.Context, acc authe.Account) {
	orders := k.accountOrders.GetAllOrders(acc.GetAddress())

	orders.Each(func(_ int, v interface{}) {
		order := v.(*types.Order)
		denomBalance := acc.SpendableCoins(ctx.BlockTime()).AmountOf(order.Source.Denom)

		order.SourceRemaining = order.Source.Amount.Sub(order.SourceFilled)
		order.SourceRemaining = sdk.MinInt(order.SourceRemaining, denomBalance)

		if order.SourceRemaining.IsZero() {
			k.deleteOrder(ctx, order)
		}
	})
}

func (k Keeper) setOrder(ctx sdk.Context, order *types.Order) {
	store := ctx.KVStore(k.key)
	bz := k.cdc.MustMarshalBinaryBare(order)
	store.Set(types.GetOrderKey(order.ID), bz)
}

func (k *Keeper) deleteOrder(ctx sdk.Context, order *types.Order) {
	store := ctx.KVStore(k.key)
	store.Delete(types.GetOrderKey(order.ID))

	k.accountOrders.RemoveOrder(order)

	instrument := k.instruments.GetInstrument(order.Source.Denom, order.Destination.Denom)
	if instrument == nil {
		return
	}

	instrument.Orders.Remove(order)
	if instrument.Orders.Empty() {
		k.instruments.RemoveInstrument(*instrument)
	}
}

func (k Keeper) GetOrdersByOwner(owner sdk.AccAddress) []types.Order {
	orders := k.accountOrders.GetAllOrders(owner)
	res := make([]types.Order, orders.Size())

	it := orders.Iterator()
	for it.Next() {
		o := it.Value().(*types.Order)
		res[it.Index()] = *o // Copy all orders, so that the calling function can't modify state.
	}

	return res
}

func (k Keeper) transferTradedAmounts(ctx sdk.Context, sourceFilled, destinationFilled sdk.Coin, passiveAccountAddr, aggressiveAccountAddr sdk.AccAddress) sdk.Error {
	var (
		passiveAccount    = k.ak.GetAccount(ctx, passiveAccountAddr)
		aggressiveAccount = k.ak.GetAccount(ctx, aggressiveAccountAddr)
	)

	// Verify that the passive order still holds the balance
	if _, anyNegative := passiveAccount.SpendableCoins(ctx.BlockTime()).SafeSub(sdk.NewCoins(destinationFilled)); anyNegative {
		return types.ErrAccountBalanceInsufficient(passiveAccount.GetAddress(), destinationFilled, passiveAccount.SpendableCoins(ctx.BlockTime()).AmountOf(destinationFilled.Denom))
	}

	// Verify that the aggressive order still holds the balance
	if _, anyNegative := aggressiveAccount.SpendableCoins(ctx.BlockTime()).SafeSub(sdk.NewCoins(sourceFilled)); anyNegative {
		return types.ErrAccountBalanceInsufficient(aggressiveAccount.GetAddress(), sourceFilled, aggressiveAccount.SpendableCoins(ctx.BlockTime()).AmountOf(sourceFilled.Denom))
	}

	// Balances appear sufficient. Do the transfers
	err := k.bk.SendCoins(ctx, aggressiveAccountAddr, passiveAccountAddr, sdk.NewCoins(sourceFilled))
	if err != nil {
		return err
	}

	err = k.bk.SendCoins(ctx, passiveAccountAddr, aggressiveAccountAddr, sdk.NewCoins(destinationFilled))
	if err != nil {
		// TODO Reverse the successful send?
		return err
	}

	return nil
}

func (k Keeper) getNextOrderNumber(ctx sdk.Context) uint64 {
	var orderID uint64
	store := ctx.KVStore(k.key)
	bz := store.Get(types.GetOrderIDGeneratorKey())
	if bz == nil {
		orderID = 0
	} else {
		err := k.cdc.UnmarshalBinaryLengthPrefixed(bz, &orderID)
		if err != nil {
			panic(err)
		}
	}

	bz = k.cdc.MustMarshalBinaryLengthPrefixed(orderID + 1)
	store.Set(types.GetOrderIDGeneratorKey(), bz)

	return orderID
}