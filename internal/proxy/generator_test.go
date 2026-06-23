package proxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
)

func TestConfigFor(t *testing.T) {
	cases := []struct {
		name string
		req  GenerateRequest
		want *ai.GenerationCommonConfig
	}{
		{"no tuning fields", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi"}, nil},
		{
			"temperature only",
			GenerateRequest{Temperature: temp(0.5)},
			&ai.GenerationCommonConfig{Temperature: 0.5},
		},
		{
			"stop sequences only",
			GenerateRequest{StopSequences: []string{"\n\n", "END"}},
			&ai.GenerationCommonConfig{StopSequences: []string{"\n\n", "END"}},
		},
		{
			"all fields",
			GenerateRequest{Temperature: temp(0.7), MaxOutputTokens: intp(256), TopP: temp(0.9), TopK: intp(40), StopSequences: []string{"STOP"}},
			&ai.GenerationCommonConfig{Temperature: 0.7, MaxOutputTokens: 256, TopP: 0.9, TopK: 40, StopSequences: []string{"STOP"}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := configFor(testCase.req)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("configFor(%+v) = %+v, want %+v", testCase.req, got, testCase.want)
			}
		})
	}
}

func TestUsageFrom(t *testing.T) {
	cases := []struct {
		name string
		in   *ai.GenerationUsage
		want *Usage
	}{
		{"nil usage", nil, nil},
		{
			"populated usage",
			&ai.GenerationUsage{InputTokens: 12, OutputTokens: 34, TotalTokens: 46},
			&Usage{Input: 12, Output: 34, Total: 46},
		},
		{
			"zero usage",
			&ai.GenerationUsage{},
			&Usage{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := usageFrom(testCase.in)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("usageFrom(%+v) = %+v, want %+v", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestGenerateResponseUsageMarshalling(t *testing.T) {
	t.Run("omits usage when nil", func(t *testing.T) {
		out, err := json.Marshal(GenerateResponse{Model: "googleai/gemini-2.5-flash", Output: "hi"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(out), "usage") {
			t.Errorf("response %s should not contain usage", out)
		}
	})
	t.Run("includes usage when set", func(t *testing.T) {
		out, err := json.Marshal(GenerateResponse{
			Model:  "googleai/gemini-2.5-flash",
			Output: "hi",
			Usage:  &Usage{Input: 12, Output: 34, Total: 46},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		const want = `"usage":{"input":12,"output":34,"total":46}`
		if !strings.Contains(string(out), want) {
			t.Errorf("response %s should contain %s", out, want)
		}
	})
}
