package billing_strategy

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectMostExpensiveUsesRawTotalCostOnly(t *testing.T) {
	selected, ok := SelectMostExpensive([]Candidate{
		{Model: "gpt-5.5", Available: true, TotalCost: 2},
		{Model: "gpt-5.6-sol-mediam", Available: true, TotalCost: 3},
	})

	require.True(t, ok)
	require.Equal(t, 1, selected)
}

func TestSelectMostExpensiveSkipsUnavailableAndInvalidCandidates(t *testing.T) {
	selected, ok := SelectMostExpensive([]Candidate{
		{Model: "missing", Available: false, TotalCost: 100},
		{Model: "nan", Available: true, TotalCost: math.NaN()},
		{Model: "valid", Available: true, TotalCost: 1},
	})

	require.True(t, ok)
	require.Equal(t, 2, selected)
}
