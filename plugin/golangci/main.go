package main

import (
	"github.com/arturmelanchyk/boolset/boolset"
	"golang.org/x/tools/go/analysis"
)

type analyzerPlugin struct{}

func (*analyzerPlugin) GetAnalyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		boolset.NewAnalyzer(),
	}
}

var AnalyzerPlugin analyzerPlugin
