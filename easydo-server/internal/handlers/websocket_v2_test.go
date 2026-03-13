package handlers

import "testing"

func TestIsValidTaskStatusTransition(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{
			name: "queued to assigned",
			from: "queued",
			to:   "assigned",
			want: true,
		},
		{
			name: "assigned to dispatching",
			from: "assigned",
			to:   "dispatching",
			want: true,
		},
		{
			name: "running to execute_success",
			from: "running",
			to:   "execute_success",
			want: true,
		},
		{
			name: "running to execute_failed",
			from: "running",
			to:   "execute_failed",
			want: true,
		},
		{
			name: "assigned to execute_success is invalid",
			from: "assigned",
			to:   "execute_success",
			want: false,
		},
		{
			name: "execute_success to running is invalid",
			from: "execute_success",
			to:   "running",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidTaskStatusTransition(tt.from, tt.to)
			if got != tt.want {
				t.Fatalf("isValidTaskStatusTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestBuildTaskUpdateIdempotencyKey(t *testing.T) {
	k1 := buildTaskUpdateIdempotencyKey(101, 1, "running", 0)
	k2 := buildTaskUpdateIdempotencyKey(101, 1, "running", 0)
	k3 := buildTaskUpdateIdempotencyKey(101, 1, "execute_success", 0)

	if k1 == "" {
		t.Fatal("idempotency key should not be empty")
	}
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got k1=%q k2=%q", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("keys for different statuses must differ, got k1=%q k3=%q", k1, k3)
	}
}

func TestBuildTaskLogChunkUniqueKey(t *testing.T) {
	k1 := buildTaskLogChunkUniqueKey(9, 2, 7)
	k2 := buildTaskLogChunkUniqueKey(9, 2, 7)
	k3 := buildTaskLogChunkUniqueKey(9, 2, 8)

	if k1 == "" {
		t.Fatal("log chunk key should not be empty")
	}
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got k1=%q k2=%q", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("keys for different seq must differ, got k1=%q k3=%q", k1, k3)
	}
}
