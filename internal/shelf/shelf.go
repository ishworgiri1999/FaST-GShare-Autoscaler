// Package shelfpack implements an online 2D bin packing using the Shelf algorithm.
// Supports insertion and removal of items, with automatic collapsing of empty shelves.
package shelf

import (
	"errors"
	"fmt"
	"sync"
)

// Item represents a packed rectangle with an ID.
type Item struct {
	ID     int     // unique identifier
	Width  float64 // normalized width (0.0 < Width ≤ 1.0)
	Height int     // height of the item
}

// Shelf represents a horizontal shelf in the bin.
type Shelf struct {
	Height    int          // height of this shelf
	UsedWidth float64      // total width used (sum of item widths)
	Items     map[int]Item // map from item ID → Item
}

// ShelfPacker packs items into shelves online.
type ShelfPacker struct {
	sync.Mutex               // protect concurrent access
	MaxHeight    int         // total bin height limit
	Shelves      []Shelf     // current shelves
	UsedHeight   int         // total height used by shelves
	nextItemID   int         // auto-incrementing item ID
	itemLocation map[int]int // map item ID → shelf index
}

// New creates a new ShelfPacker with given maxHeight (e.g., 100).
func NewShelf(maxHeight int) *ShelfPacker {
	return &ShelfPacker{
		MaxHeight:    maxHeight,
		Shelves:      []Shelf{},
		UsedHeight:   0,
		nextItemID:   1,
		itemLocation: make(map[int]int),
	}
}

// Insert places an item of given width and height, returning its item ID or error.
func (sp *ShelfPacker) Insert(width float64, height int) (int, error) {
	sp.Lock()
	defer sp.Unlock()

	if width <= 0 || width > 1.0 {
		return 0, errors.New("width must be >0 and ≤1.0")
	}
	if height <= 0 || height > sp.MaxHeight {
		return 0, errors.New("height must be >0 and ≤ bin max height")
	}

	itemID := sp.nextItemID
	sp.nextItemID++
	item := Item{ID: itemID, Width: width, Height: height}

	// try existing shelves (first-fit)
	for idx := range sp.Shelves {
		shelf := &sp.Shelves[idx]
		if height <= shelf.Height && shelf.UsedWidth+width <= 1.0 {
			if shelf.Items == nil {
				shelf.Items = make(map[int]Item)
			}
			shelf.Items[itemID] = item
			shelf.UsedWidth += width
			sp.itemLocation[itemID] = idx
			return itemID, nil
		}
	}

	// open new shelf
	remaining := sp.MaxHeight - sp.UsedHeight
	if height > remaining {
		return 0, errors.New("no space for new shelf: height exceeds remaining bin height")
	}
	newShelf := Shelf{
		Height:    height,
		UsedWidth: width,
		Items:     map[int]Item{itemID: item},
	}
	sp.Shelves = append(sp.Shelves, newShelf)
	sp.UsedHeight += height
	sp.itemLocation[itemID] = len(sp.Shelves) - 1
	return itemID, nil
}

func (sp *ShelfPacker) CanFit(width float64, height int) (bool, error) {

	for _, shelf := range sp.Shelves {
		if height <= shelf.Height && shelf.UsedWidth+width <= 1.0 {
			return true, nil
		}
	}
	remaining := sp.MaxHeight - sp.UsedHeight

	if height <= remaining {
		return true, nil
	}

	return false, fmt.Errorf("no space for new shelf: height exceeds remaining bin height :remaining %d, height %d", remaining, height)
}

// Remove deletes the item with the given ID, automatically collapsing empty shelves, and returns its former shelf index.
func (sp *ShelfPacker) Remove(itemID int) (int, error) {
	sp.Lock()
	defer sp.Unlock()

	shelfIdx, ok := sp.itemLocation[itemID]
	if !ok {
		return -1, errors.New("item ID not found")
	}
	shelf := &sp.Shelves[shelfIdx]
	item, exists := shelf.Items[itemID]
	if !exists {
		return -1, errors.New("item data missing in shelf")
	}
	delete(shelf.Items, itemID)
	shelf.UsedWidth -= item.Width
	delete(sp.itemLocation, itemID)

	// auto-collapse empty shelf
	if len(shelf.Items) == 0 {
		sp.UsedHeight -= shelf.Height
		sp.Shelves = append(sp.Shelves[:shelfIdx], sp.Shelves[shelfIdx+1:]...)
		for id, idx := range sp.itemLocation {
			if idx > shelfIdx {
				sp.itemLocation[id] = idx - 1
			}
		}
	}

	return shelfIdx, nil
}

func (sp *ShelfPacker) IsEmpty() bool {
	return len(sp.Shelves) == 0
}

// maxPlacedCount returns the maximum number of items that can be placed in the shelves of fixed width and height
func (sp *ShelfPacker) MaxInsertableItems(width float64, height int) int {
	if width <= 0 || width > 1.0 || height <= 0 || height > sp.MaxHeight {
		return 0
	}

	count := 0
	usedHeight := 0

	// Count how many more items can fit in existing shelves
	for _, shelf := range sp.Shelves {
		if height <= shelf.Height {
			remainingWidth := 1.0 - shelf.UsedWidth
			count += int(remainingWidth / width)
		}
		usedHeight += shelf.Height
	}

	// Count how many items can fit in new shelves (if any)
	remainingHeight := sp.MaxHeight - usedHeight
	numNewShelves := remainingHeight / height
	itemsPerShelf := int(1.0 / width)
	count += numNewShelves * itemsPerShelf

	return count
}

// Reset clears all packed items and shelves.
func (sp *ShelfPacker) Reset() {
	sp.Lock()
	defer sp.Unlock()
	sp.Shelves = nil
	sp.UsedHeight = 0
	sp.nextItemID = 1
	sp.itemLocation = make(map[int]int)
}
