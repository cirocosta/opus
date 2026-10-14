// Ported from libopus 1.6.1 silk/sort.c.

package silk

// insertionSortIncreasing is silk_insertion_sort_increasing: sort the
// first K values of a in increasing order and write their original
// indices to idx.
func insertionSortIncreasing(a []int32, idx []int, l, k int) {
	// Write start indices in index vector.
	for i := 0; i < k; i++ {
		idx[i] = i
	}

	// insert each candidate into the sorted prefix, keeping only the best k.
	for i := 1; i < l; i++ {
		value := a[i]
		if i >= k && value >= a[k-1] {
			continue
		}
		j := min(i, k-1) - 1
		for ; j >= 0 && value < a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
}

// insertionSortIncreasingAllValuesInt16 is
// silk_insertion_sort_increasing_all_values_int16.
func insertionSortIncreasingAllValuesInt16(a []int16, l int) {
	// Sort vector elements by value, increasing order.
	for i := 1; i < l; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value < a[j]; j-- {
			a[j+1] = a[j] // Shift value
		}
		a[j+1] = value // Write value
	}
}
