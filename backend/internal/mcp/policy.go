// Package mcp reserves the policy boundary for future agent operations.
package mcp

type Risk string

const (
	RiskReadOnly Risk = "read_only"
	RiskLow      Risk = "low"
	RiskHigh     Risk = "high"
)

type Tool struct {
	Name string
	Risk Risk
}
type Policy interface{ Allow(tool Tool) bool }
