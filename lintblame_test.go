package main

import (
    "testing"
    "time"
    "fmt"
)

var blah = fmt.Sprintf("stop complaining")

func TestSortaSorted(t *testing.T) {
    mt := ModifiedTimes{}
    mt.TimeMap = make(map[string]time.Time, 3)
    mt.TimeMap["abc"] = time.Now().AddDate(0, 0, -1)
    mt.TimeMap["def"] = time.Now()
    mt.TimeMap["ghi"] = time.Now().AddDate(0, 0, -2)

    result := mt.SortaSorted()
    if result[0] != "ghi" || result[1] != "abc" || result[2] != "def" {
        t.Error("Bad order")
    }
}
