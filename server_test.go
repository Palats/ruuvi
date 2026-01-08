package main

import (
	"testing"
)

func TestDataFormat5(t *testing.T) {
	adv, err := decodeBluetoothData("0201061BFF990405104A2FA2B2CEFFF4000C0418A1B61B8BE4D4A5CBFD4924")
	if err != nil {
		t.Fatalf("decoding failure: %v", err)
	}
	if adv.Data5 == nil {
		t.Fatal("missing data")
	}
}

func TestDataFormatE1(t *testing.T) {
	adv, err := decodeBluetoothData("2BFF9904E111D22BA8B2DA0005000A000F001103295000FFFFFFFFFFFF39882AB8FFFFFFFFFFF844577FBD89030398FC")
	if err != nil {
		t.Fatalf("decoding failure: %v", err)
	}
	if adv.DataE1 == nil {
		t.Fatal("missing data")
	}
}
