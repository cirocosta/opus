// Ported from libopus 1.6.1 silk/float/sort_FLP.c.

package silk

// insertionSortDecreasingFLP is silk_insertion_sort_decreasing_FLP:
// insertion sort (fast for already almost sorted arrays). Only the first
// K values are guaranteed to be sorted.
func insertionSortDecreasingFLP(a []float32, idx []int, l, k int) {
	// Write start indices in index vector.
	for i := 0; i < k; i++ {
		idx[i] = i
	}

	// insert each candidate into the sorted prefix, keeping only the best k.
	for i := 1; i < l; i++ {
		value := a[i]
		if i >= k && !(value > a[k-1]) {
			continue
		}
		j := min(i, k-1) - 1
		for ; j >= 0 && value > a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
}
