package automations_test

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
)

func TestDecide(t *testing.T) {
	price := 500.0
	below := automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: price},
	}
	available := automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionAvailable},
	}
	significant := automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionSignificant},
	}
	out := "Out of stock.\n{\"available\": false}"
	inStock := "Back in stock.\n```json\n{\"available\": true, \"price\": 420}\n```"
	same := "Still $420."

	cases := []struct {
		name     string
		note     automations.Notification
		result   string
		previous *string
		told     bool
		notify   bool
		body     string
	}{
		{name: "always", note: automations.Notification{Mode: automations.NotifyAlways}, result: "Ready.", notify: true, body: "Ready."},
		{name: "store only", note: automations.Notification{Mode: automations.NotifyNone}, result: "Ready.", notify: false},
		{name: "change baseline", note: automations.Notification{Mode: automations.NotifyOnChange}, result: same, notify: false},
		{name: "change same text", note: automations.Notification{Mode: automations.NotifyOnChange}, result: same + "\n", previous: &same, notify: false},
		{name: "change different text", note: automations.Notification{Mode: automations.NotifyOnChange}, result: "Now $390.", previous: &same, notify: true, body: "Now $390."},
		{name: "price below", note: below, result: "The laptop is $420.\n{\"price\": 420}", notify: true, body: "The laptop is $420."},
		{name: "price equal", note: below, result: "{\"price\": 500}", notify: false},
		{name: "price above the below-threshold", note: below, result: "{\"price\": 640}", notify: false},
		{name: "missing price", note: below, result: "about five hundred", notify: false},
		{name: "becomes available", note: available, result: inStock, previous: &out, told: true, notify: true, body: "Back in stock."},
		{name: "already available", note: available, result: inStock, previous: strPtr("{\"available\": true}"), told: true, notify: false},
		{name: "in stock was never announced", note: available, result: "It is in stock.", previous: strPtr("It is in stock."), notify: true, body: "It is in stock."},
		{name: "in stock in prose", note: available, result: "Based on the information available, it seems that Nintendo Switch 2 is in stock at Costco. You can find specific models and bundles available on their website, or check in-store.", notify: true, body: "Nintendo Switch 2 is in stock"},
		{name: "question is not a stock confirmation", note: available, result: "Check whether it is in stock. Based on the information available, visit the website.", notify: false},
		{name: "json false wins", note: available, result: "It is in stock.\n{\"available\": false}", notify: false},
		{name: "not available", note: available, result: out, notify: false},
		{name: "significant", note: significant, result: "Worth a look.\n{\"significant\": true}", notify: true, body: "Worth a look."},
		{name: "not significant", note: significant, result: "{\"significant\": false}", notify: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := automations.Decide(tc.note, tc.result, tc.previous, tc.told)
			if got.Notify != tc.notify {
				t.Fatalf("notify = %v, want %v (%s)", got.Notify, tc.notify, got.Reason)
			}
			if tc.notify && !strings.Contains(got.Notice.Body, tc.body) {
				t.Fatalf("body = %q, want it to contain %q", got.Notice.Body, tc.body)
			}
		})
	}
}

func TestConditionValidation(t *testing.T) {
	err := (automations.Notification{Mode: automations.NotifyOnCondition}).Validate()
	if err == nil {
		t.Fatal("expected a missing condition to fail")
	}
	err = (automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: "around", Value: 1},
	}).Validate()
	if err == nil {
		t.Fatal("expected an unknown comparison to fail")
	}
	err = (automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionAvailable},
	}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	for currency, valid := range map[string]bool{"": true, "EUR": true, "KRW": true, "eur": false, "€": false, "EURO": false} {
		err = (automations.Notification{
			Mode:      automations.NotifyOnCondition,
			Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500, Currency: currency},
		}).Validate()
		if (err == nil) != valid {
			t.Fatalf("currency %q: err = %v, want valid = %v", currency, err, valid)
		}
	}
}

func TestThresholdNoticeNamesTheCurrency(t *testing.T) {
	n := automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500, Currency: "EUR"},
	}
	got := automations.Decide(n, `{"price": 449}`, nil, false)
	if !got.Notify || got.Notice.Body != "Price is 449 EUR." {
		t.Fatalf("decision = %+v, want a notice that says the price in euros", got)
	}
}

func strPtr(s string) *string { return &s }
