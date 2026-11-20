package scm

import "testing"

func TestKindsDescribeAvailableDeliveryAdapters(t *testing.T) {
	kinds := Kinds()
	if len(kinds) != 3 {
		t.Fatalf("kinds: %+v", kinds)
	}
	for _, k := range kinds {
		if _, ok := factory(k.ID); !ok {
			t.Fatalf("unregistered: %+v", k)
		}
		if k.Name == "" || k.RepositoryHint == "" || k.CredentialHelp == "" || !k.Push || !k.CreatePullRequest {
			t.Fatalf("incomplete: %+v", k)
		}
		if k.ID == "bitbucket-dc" && (!k.APIURLRequired || k.APIURLHint != "https://bitbucket.example.com/context") {
			t.Fatalf("DC site URL: %+v", k)
		}
	}
	kinds[0].Name = "changed"
	if Kinds()[0].Name == "changed" {
		t.Fatal("caller changed shared metadata")
	}
}
