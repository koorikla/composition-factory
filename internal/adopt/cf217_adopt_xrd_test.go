package adopt

import (
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
)

func TestCF217_AdoptXRDComplementsBaseBlueprint(t *testing.T) {
	// 1. Read original pipeline blueprint and emit its canonical XRD
	origBPBytes, err := os.ReadFile("../../testdata/xqueue-pipeline.cf.yaml")
	if err != nil {
		t.Fatalf("read testdata blueprint: %v", err)
	}
	origBP, err := blueprint.Parse(origBPBytes)
	if err != nil {
		t.Fatalf("parse testdata blueprint: %v", err)
	}
	xrdYAML, err := emit.XRD(origBP)
	if err != nil {
		t.Fatalf("emit XRD: %v", err)
	}

	// 2. Read the composition golden file (composition only, no XRD)
	compYAML, err := os.ReadFile("../../testdata/xqueue-pipeline.composition.golden.yaml")
	if err != nil {
		t.Fatalf("read golden composition: %v", err)
	}

	// Step 1 of repro: Adopt composition alone
	bp, report, err := Adopt(compYAML, Options{})
	if err != nil {
		t.Fatalf("adopt composition alone: %v", err)
	}
	if !report.HasTrueLoss() {
		t.Fatalf("expected true loss when adopting composition without XRD; report: %+v", report.Drops)
	}

	foundMaxMessageSizeLoss := false
	for _, d := range report.Drops {
		if d.Path == "xrd.parameters.maxMessageSize" {
			foundMaxMessageSizeLoss = true
			if !strings.Contains(d.Reason, "combine XRD and Composition") && !strings.Contains(d.Reason, "supply XRD and Composition") {
				t.Errorf("expected loss report to hint how to supply XRD, got: %q", d.Reason)
			}
		}
	}
	if !foundMaxMessageSizeLoss {
		t.Errorf("expected drop for xrd.parameters.maxMessageSize, got drops: %+v", report.Drops)
	}

	// Step 2 of repro: Import the XRD to complement the adopted blueprint
	complementedBP, compReport, err := Adopt(xrdYAML, Options{
		BaseBlueprint: bp,
	})
	if err != nil {
		t.Fatalf("adopt XRD to complement base blueprint failed: %v", err)
	}

	if compReport != nil && compReport.HasTrueLoss() {
		t.Errorf("complemented blueprint should have no true loss, got: %+v", compReport.Drops)
	}

	param, ok := complementedBP.Spec.XRD.Parameters["maxMessageSize"]
	if !ok {
		t.Fatalf("expected parameter maxMessageSize in complemented blueprint")
	}
	if param.Type != "integer" {
		t.Errorf("expected maxMessageSize type 'integer', got %q", param.Type)
	}
	if param.Default != "2048" {
		t.Errorf("expected maxMessageSize default '2048', got %q", param.Default)
	}

	// location was present in XRD but not used in composition; it must now be recovered
	locParam, ok := complementedBP.Spec.XRD.Parameters["location"]
	if !ok {
		t.Errorf("expected location parameter from XRD in complemented blueprint")
	} else if locParam.Type != "string" || !locParam.Required {
		t.Errorf("expected location parameter to be required string, got %+v", locParam)
	}

	// Resources and wiring must be preserved
	if len(complementedBP.Spec.Resources) != len(origBP.Spec.Resources) {
		t.Errorf("resources count = %d, want %d", len(complementedBP.Spec.Resources), len(origBP.Spec.Resources))
	}
}

func TestCF217_AdoptXRDWithoutBaseBlueprintProvidesGuidance(t *testing.T) {
	origBPBytes, err := os.ReadFile("../../testdata/xqueue-pipeline.cf.yaml")
	if err != nil {
		t.Fatalf("read testdata blueprint: %v", err)
	}
	origBP, err := blueprint.Parse(origBPBytes)
	if err != nil {
		t.Fatalf("parse testdata blueprint: %v", err)
	}
	xrdYAML, err := emit.XRD(origBP)
	if err != nil {
		t.Fatalf("emit XRD: %v", err)
	}

	_, _, err = Adopt(xrdYAML, Options{BaseBlueprint: nil})
	if err == nil {
		t.Fatalf("expected error when adopting XRD without Composition or BaseBlueprint, got nil")
	}
	if err.Error() != "no Composition document found in manifest" {
		t.Errorf("expected error 'no Composition document found in manifest', got: %v", err)
	}
}
