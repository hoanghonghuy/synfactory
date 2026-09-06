package postgres

import "testing"

func TestEstimateRuntimeCostMicroUSD(t *testing.T) {
	pricing := RuntimePricing{
		InputMicroUSDPerMillion:  2_000_000,
		OutputMicroUSDPerMillion: 8_000_000,
		RequestMicroUSD:          50,
	}
	cost, err := EstimateRuntimeCostMicroUSD(pricing, 2, 250_000, 125_000)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 1_500_100 {
		t.Fatalf("cost = %d, want 1500100", cost)
	}
}

func TestEstimateRuntimeCostRejectsNegativeUsage(t *testing.T) {
	if _, err := EstimateRuntimeCostMicroUSD(RuntimePricing{}, 1, -1, 0); err == nil {
		t.Fatal("expected negative usage to fail")
	}
}
