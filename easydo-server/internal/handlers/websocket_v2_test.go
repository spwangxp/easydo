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
			name: "pending to running",
			from: "pending",
			to:   "running",
			want: true,
		},
		{
			name: "running to success",
			from: "running",
			to:   "success",
			want: true,
		},
		{
			name: "running to failed",
			from: "running",
			to:   "failed",
			want: true,
		},
		{
			name: "pending to success is invalid",
			from: "pending",
			to:   "success",
			want: false,
		},
		{
			name: "success to running is invalid",
			from: "success",
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
	k3 := buildTaskUpdateIdempotencyKey(101, 1, "success", 0)

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
