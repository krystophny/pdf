package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestGetPlainTextHonorsTJWordSpacing(t *testing.T) {
	body := strings.Join([]string{
		"BT",
		"/F1 12 Tf",
		"72 720 Td",
		"[(Neoclassical)-260(Toroidal)-260(Viscosity)] TJ",
		"ET",
	}, "\n")
	reader := newTestReader(t, body)

	got, err := reader.Page(1).GetPlainText(nil)
	if err != nil {
		t.Fatalf("GetPlainText: %v", err)
	}
	if !strings.Contains(got, "Neoclassical Toroidal Viscosity") {
		t.Fatalf("text = %q", got)
	}
	if strings.Contains(got, "NeoclassicalToroidal") {
		t.Fatalf("text has glued words: %q", got)
	}
}

func TestGetPlainTextDoesNotTurnTJKerningIntoSpace(t *testing.T) {
	body := strings.Join([]string{
		"BT",
		"/F1 12 Tf",
		"72 720 Td",
		"[(AV)-40(A)] TJ",
		"ET",
	}, "\n")
	reader := newTestReader(t, body)

	got, err := reader.Page(1).GetPlainText(nil)
	if err != nil {
		t.Fatalf("GetPlainText: %v", err)
	}
	if strings.Contains(got, "AV A") {
		t.Fatalf("kerning became a space: %q", got)
	}
	if !strings.Contains(got, "AVA") {
		t.Fatalf("text = %q", got)
	}
}

func TestTextLayoutReadsColumnsAfterHeader(t *testing.T) {
	body := strings.Join([]string{
		"BT",
		"/F1 12 Tf",
		"1 0 0 1 72 740 Tm (Title) Tj",
		"1 0 0 1 72 700 Tm (left one) Tj",
		"1 0 0 1 320 700 Tm (right one) Tj",
		"1 0 0 1 72 684 Tm (left two) Tj",
		"1 0 0 1 320 684 Tm (right two) Tj",
		"ET",
	}, "\n")
	reader := newTestReader(t, body)

	layout, err := reader.Page(1).TextLayout()
	if err != nil {
		t.Fatalf("TextLayout: %v", err)
	}
	got := layout.PlainText()
	want := strings.Join([]string{
		"Title",
		"left one",
		"left two",
		"right one",
		"right two",
	}, "\n")
	if got != want {
		t.Fatalf("layout text:\n%s\nwant:\n%s", got, want)
	}
}

func newTestReader(t *testing.T, content string) *Reader {
	t.Helper()
	data := buildTextPDF(content)
	reader, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return reader
}

func buildTextPDF(content string) []byte {
	w := &testPDFWriter{offsets: []int{0}}
	w.body.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	catalogID := w.newObjectID()
	pagesID := w.newObjectID()
	fontID := w.newObjectID()
	pageID := w.newObjectID()
	contentID := w.newObjectID()

	w.writeObject(catalogID, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesID))
	w.writeObject(pagesID, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", pageID))
	w.writeObject(fontID, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	w.writeObject(pageID, fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesID, fontID, contentID,
	))
	w.writeStream(contentID, content)

	return w.finalize(catalogID)
}

type testPDFWriter struct {
	body    bytes.Buffer
	offsets []int
	count   int
}

func (w *testPDFWriter) newObjectID() int {
	w.count++
	w.offsets = append(w.offsets, 0)
	return w.count
}

func (w *testPDFWriter) writeObject(id int, body string) {
	w.offsets[id] = w.body.Len()
	fmt.Fprintf(&w.body, "%d 0 obj\n%s\nendobj\n", id, body)
}

func (w *testPDFWriter) writeStream(id int, content string) {
	w.offsets[id] = w.body.Len()
	fmt.Fprintf(&w.body, "%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", id, len(content), content)
}

func (w *testPDFWriter) finalize(catalogID int) []byte {
	xrefStart := w.body.Len()
	fmt.Fprintf(&w.body, "xref\n0 %d\n", w.count+1)
	w.body.WriteString("0000000000 65535 f \n")
	for i := 1; i <= w.count; i++ {
		fmt.Fprintf(&w.body, "%010d 00000 n \n", w.offsets[i])
	}
	fmt.Fprintf(&w.body, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", w.count+1, catalogID, xrefStart)
	return w.body.Bytes()
}
