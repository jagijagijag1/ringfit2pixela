package main

import (
	"reflect"
	"testing"
)

func TestHttp(t *testing.T) {
	actual, err := getImageURL("https://t.co/3KVqTlU4vZ")
	if err != nil {
		t.Error(err)
	}

	expected := []string{"https://pbs.twimg.com/media/EPDdusOW4AgnEkY.jpg:large"}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("got: %v\nwant: %v", actual, expected)
	}
}

func TestDateStr(t *testing.T) {
	actual := setDateStr("10/21")
	expected := "20191021"
	if actual != expected {
		t.Errorf("got: %v\nwant: %v", actual, expected)
	}

	actual = setDateStr("01/21")
	expected = "20200121"
	if actual != expected {
		t.Errorf("got: %v\nwant: %v", actual, expected)
	}
}
