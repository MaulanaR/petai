package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestBuild(t *testing.T) {
	b, err := Build("Notulensi Meeting — Rabu", "# Notulensi Meeting — Rabu\n## Peserta\n- Budi & Ani <PIC>\n\nCatatan \"penting\"")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			doc = string(raw)
		}
	}
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/_rels/document.xml.rels"} {
		if !names[n] {
			t.Errorf("missing %s", n)
		}
	}
	if err := xml.Unmarshal([]byte(doc), new(struct{})); err != nil {
		t.Fatalf("document.xml not well-formed: %v", err)
	}
	if strings.Count(doc, "Notulensi Meeting") != 1 {
		t.Error("title duplicated or missing")
	}
	for _, want := range []string{`w:val="Title"`, `w:val="Heading2"`, `w:val="ListBullet"`, "Budi &amp; Ani &lt;PIC&gt;", "Catatan &#34;penting&#34;"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml missing %q", want)
		}
	}
}
