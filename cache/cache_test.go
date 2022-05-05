package cache

import (
	"testing"
)

func TestCache1(t *testing.T) {
	// new LRU cache with capacity of 2
	capacity := 2
	lru := NewLruCache(capacity)

	// test standard put and get
	lru.Put(1,1)
	lru.Put(2,2)
	actual, err := lru.Get(1)
	expected := 1
	AssertEqualNoError(t, expected, actual, err)

	// test eviction of 2
	lru.Put(3,3)
	actual, err = lru.Get(2)
	expected = -1
	AssertErrorNotNil(t, expected, actual, err)

	// test eviction of 1
	lru.Put(4,4)
	actual, err = lru.Get(1)
	expected = -1
	AssertErrorNotNil(t, expected, actual, err)	

	// test 3 still in cache
	actual, err = lru.Get(3)
	expected = 3
	AssertEqualNoError(t, expected, actual, err)

	// test 4 still in cache
	actual, err = lru.Get(4)
	expected = 4
	AssertEqualNoError(t, expected, actual, err)
}

func AssertEqualNoError(t *testing.T, expected interface{}, actual interface{}, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("error: %v", err)
	}
	if expected != actual {
		t.Errorf("expected %d, received %d", expected, actual)
	}
}

func AssertErrorNotNil(t *testing.T, expected interface{}, actual interface{}, err error) {
	if err == nil {
		t.Errorf("element was found in cache after it should have been evicted")
	}
}
