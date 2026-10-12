package codexsdk

import (
	"reflect"
	"runtime"
	"sync"
)

// NotificationRetentionBudget limits SDK-owned representation slots and their
// conservative retained Go byte charge across one client. Zero selects defaults;
// negative values are invalid. Neither dimension can be disabled.
type NotificationRetentionBudget struct {
	MaxCount int
	MaxBytes int64
}

const (
	DefaultNotificationRetentionCount       = 65536
	DefaultNotificationRetentionBytes int64 = 64 << 20
)

type notificationRetention struct {
	mu    sync.Mutex
	limit NotificationRetentionBudget
	count int
	bytes int64
}

type retentionLease struct {
	budget *notificationRetention
	count  int
	bytes  int64
	refs   int
}

func (b *notificationRetention) reserve(count int, bytes int64) (*retentionLease, error) {
	if b == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if count > b.limit.MaxCount-b.count || bytes > b.limit.MaxBytes-b.bytes {
		return nil, ErrNotificationRetention
	}
	b.count += count
	b.bytes += bytes
	return &retentionLease{budget: b, count: count, bytes: bytes, refs: 1}, nil
}

func (l *retentionLease) retain() {
	if l == nil {
		return
	}
	l.budget.mu.Lock()
	if l.refs <= 0 {
		l.budget.mu.Unlock()
		panic("codexsdk: released retention ownership reused")
	}
	l.refs++
	l.budget.mu.Unlock()
}

func (l *retentionLease) release() {
	if l == nil {
		return
	}
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	l.refs--
	if l.refs < 0 {
		panic("codexsdk: retention ownership released twice")
	}
	if l.refs == 0 {
		b.count -= l.count
		b.bytes -= l.bytes
	}
}

// The cleanup argument must never refer back to the run: completed histories
// stay charged until their last SDK cursor becomes unreachable, independently
// of client closure. Application-returned snapshots are application-owned.
type retentionOwners struct {
	mu     sync.Mutex
	leases []*retentionLease
}

func (o *retentionOwners) add(l *retentionLease) {
	if l == nil {
		return
	}
	l.retain()
	o.mu.Lock()
	o.leases = append(o.leases, l)
	o.mu.Unlock()
}

func (o *retentionOwners) release() {
	o.mu.Lock()
	leases := o.leases
	o.leases = nil
	o.mu.Unlock()
	for _, l := range leases {
		l.release()
	}
}

func installRunRetention(s *exactRunState) {
	s.retained = &retentionOwners{}
	runtime.AddCleanup(s, (*retentionOwners).release, s.retained)
}

// retainedValueBytes includes Go headers, pointees, strings, slice capacity and
// deliberately generous map entry overhead. Shared backing storage is charged
// repeatedly. This is a deterministic admission charge, not a heap profiler.
// Generated protocol values and inbound JSON form acyclic trees.
func retainedValueBytes(value any) int64 { return retainedReflectBytes(reflect.ValueOf(value)) + 256 }
func retainedReflectBytes(v reflect.Value) int64 {
	if !v.IsValid() {
		return 0
	}
	n := int64(v.Type().Size())
	switch v.Kind() {
	case reflect.String:
		n += int64(v.Len())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			n += retainedReflectBytes(v.Elem())
		}
	case reflect.Slice:
		n += int64(v.Cap()) * int64(v.Type().Elem().Size())
		for i := 0; i < v.Len(); i++ {
			n += retainedReflectBytes(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			n += retainedReflectBytes(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			n += retainedReflectBytes(v.Field(i))
		}
	case reflect.Map:
		n += 256
		it := v.MapRange()
		for it.Next() {
			n += 128 + retainedReflectBytes(it.Key()) + retainedReflectBytes(it.Value())
		}
	}
	return n
}
