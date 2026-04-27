package config

import (
	"reflect"
	"testing"
)

func TestInit_BindsBootstrapDockerHubMirrorsEnv(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", "https://mirror-a.example, https://mirror-b.example")
	Init()

	if got := Config.GetString("buildkit.bootstrap_dockerhub_mirrors"); got != "https://mirror-a.example, https://mirror-b.example" {
		t.Fatalf("bootstrap mirrors=%q, want env value", got)
	}
}

func TestBootstrapDockerHubMirrors_ParsesEnvList(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", " https://mirror-a.example ,https://mirror-b.example ,, ")
	Init()

	got := BootstrapDockerHubMirrors()
	want := []string{"https://mirror-a.example", "https://mirror-b.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mirrors=%v, want=%v", got, want)
	}
}

func TestBootstrapDockerHubMirrors_EmptyEnvReturnsBuiltInDefaults(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", "")
	Init()

	got := BootstrapDockerHubMirrors()
	want := []string{
		"https://docker.1ms.run/",
		"https://hub-mirror.c.163.com/",
		"https://docker.mirrors.ustc.edu.cn/",
		"https://docker.m.daocloud.io/",
		"https://mirror.aliyuncs.com/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mirrors=%v, want=%v", got, want)
	}
}
