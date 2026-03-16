package safecast

import (
	"fmt"
	"math"
	"os"
)

func IntToUint32(v int) (uint32, error) {
	if v < 0 || uint64(v) > math.MaxUint32 {
		return 0, fmt.Errorf("value %d out of uint32 range", v)
	}
	return uint32(v), nil
}

func Int64ToUint32(v int64) (uint32, error) {
	if v < 0 || uint64(v) > math.MaxUint32 {
		return 0, fmt.Errorf("value %d out of uint32 range", v)
	}
	return uint32(v), nil
}

func Uint64ToUint32(v uint64) (uint32, error) {
	if v > math.MaxUint32 {
		return 0, fmt.Errorf("value %d out of uint32 range", v)
	}
	return uint32(v), nil
}

func Uint64ToUint8(v uint64) (uint8, error) {
	if v > math.MaxUint8 {
		return 0, fmt.Errorf("value %d out of uint8 range", v)
	}
	return uint8(v), nil
}

func Uint64ToInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("value %d out of int64 range", v)
	}
	return int64(v), nil
}

func Int64ToFileMode(v int64) (os.FileMode, error) {
	if v < 0 || uint64(v) > math.MaxUint32 {
		return 0, fmt.Errorf("file mode %d out of range", v)
	}
	return os.FileMode(uint32(v)), nil
}
