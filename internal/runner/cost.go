package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
)

const costScanWindow = 32 * 1024

type hermesCostLine struct {
	HermesCost *struct {
		TokensIn    int     `json:"tokens_in"`
		TokensOut   int     `json:"tokens_out"`
		USDEstimate float64 `json:"usd_estimate"`
	} `json:"hermes_cost"`
}

func ParseHermesCost(stderr []byte) *Cost {
	if len(stderr) > costScanWindow {
		stderr = stderr[len(stderr)-costScanWindow:]
	}
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || !bytes.Contains(line, []byte("hermes_cost")) {
			continue
		}
		var parsed hermesCostLine
		if err := json.Unmarshal(line, &parsed); err != nil || parsed.HermesCost == nil {
			continue
		}
		return &Cost{
			TokensIn:    parsed.HermesCost.TokensIn,
			TokensOut:   parsed.HermesCost.TokensOut,
			USDEstimate: parsed.HermesCost.USDEstimate,
			Source:      HermesAgentCostSource,
		}
	}
	return nil
}
