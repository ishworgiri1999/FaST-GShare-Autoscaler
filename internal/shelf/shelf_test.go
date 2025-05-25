package shelf

import (
	"fmt"
	"testing"
)

// visualizeShelves prints a simple text visualization of the shelves and items.
func visualizeShelves(sp *ShelfPacker) string {
	result := ""
	for i, shelf := range sp.Shelves {
		result += fmt.Sprintf("Shelf %d (height=%d, used=%.2f): ", i, shelf.Height, shelf.UsedWidth)
		used := 0.0
		for _, item := range shelf.Items {
			result += fmt.Sprintf("[ID:%d w:%.2f h:%d] ", item.ID, item.Width, item.Height)
			used += item.Width
		}
		result += fmt.Sprintf("| Remaining: %.2f\n", 1.0-used)
	}
	result += fmt.Sprintf("Total used height: %d/%d\n", sp.UsedHeight, sp.MaxHeight)
	return result
}

func TestShelfPacker_InsertAndRemove(t *testing.T) {
	sp := NewShelf(10)

	// Insert first item
	_, err := sp.Insert(0.5, 3)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if len(sp.Shelves) != 1 {
		t.Errorf("Expected 1 shelf, got %d", len(sp.Shelves))
	}

	// Insert second item, fits in same shelf
	id2, err := sp.Insert(0.4, 2)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if len(sp.Shelves) != 2 {
		t.Errorf("Expected 2 shelves, got %d", len(sp.Shelves))
	}

	// Insert third item, new shelf
	_, err = sp.Insert(0.6, 4)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if len(sp.Shelves) != 3 {
		t.Errorf("Expected 3 shelves, got %d", len(sp.Shelves))
	}

	// Remove item from middle shelf
	removedIdx, err := sp.Remove(id2)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if removedIdx != 1 {
		t.Errorf("Expected removed from shelf 1, got %d", removedIdx)
	}
	if len(sp.Shelves) != 2 {
		t.Errorf("Expected 2 shelves after collapse, got %d", len(sp.Shelves))
	}

	// Remove non-existent item
	_, err = sp.Remove(999)
	if err == nil {
		t.Error("Expected error for non-existent item ID")
	}

	// Reset
	sp.Reset()
	if len(sp.Shelves) != 0 || sp.UsedHeight != 0 {
		t.Error("Reset did not clear shelves and height")
	}
}

func TestShelfPacker_InsertErrors(t *testing.T) {
	sp := NewShelf(5)
	_, err := sp.Insert(0, 2)
	if err == nil {
		t.Error("Expected error for zero width")
	}
	_, err = sp.Insert(1.1, 2)
	if err == nil {
		t.Error("Expected error for width > 1.0")
	}
	_, err = sp.Insert(0.5, 0)
	if err == nil {
		t.Error("Expected error for zero height")
	}
	_, err = sp.Insert(0.5, 6)
	if err == nil {
		t.Error("Expected error for height > maxHeight")
	}
}

func TestShelfPacker_Visualization(t *testing.T) {
	sp := NewShelf(10)
	sp.Insert(0.3, 2)
	sp.Insert(0.7, 2)
	sp.Insert(0.5, 3)
	sp.Insert(0.5, 3)
	vis := visualizeShelves(sp)
	if len(vis) == 0 {
		t.Error("Visualization should not be empty")
	}
	fmt.Println("\nShelfPacker visualization:\n" + vis)
}
