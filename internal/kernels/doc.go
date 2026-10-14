// Package kernels implements allocation-free numerical kernels for Opus.
//
// Kernels preserve the scalar order of operations, including intermediate
// floating-point rounding. Architecture backends vectorize independent
// outputs instead of reassociating reductions. The purego build tag selects
// the portable implementation.
package kernels
