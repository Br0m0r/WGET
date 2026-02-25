package parser

import "testing"

func TestExtractLinks_Correctness(t *testing.T) {
	htmlDoc := []byte(`
<html>
  <head>
    <link rel="stylesheet" href="/assets/site.css">
    <script src="https://cdn.example.com/app.js"></script>
    <style>
      body { background-image: url('/assets/bg.png'); }
    </style>
  </head>
  <body style="background:url('images/body.png')">
    <a href="/docs/page1.html#section">Doc 1</a>
    <a href="page2.html">Doc 2</a>
    <img src="images/logo.png">
    <source srcset="img-small.png 1x, img-large.png 2x">
    <a href="mailto:ops@example.com">mail</a>
    <a href="javascript:void(0)">js</a>
  </body>
</html>`)

	links, err := ExtractLinks("https://docs.example.com/root/index.html", htmlDoc)
	if err != nil {
		t.Fatalf("ExtractLinks returned error: %v", err)
	}

	want := map[string]bool{
		"https://docs.example.com/assets/site.css":      true,
		"https://cdn.example.com/app.js":                true,
		"https://docs.example.com/docs/page1.html":      true,
		"https://docs.example.com/root/page2.html":      true,
		"https://docs.example.com/root/images/logo.png": true,
		"https://docs.example.com/root/img-small.png":   true,
		"https://docs.example.com/root/img-large.png":   true,
		"https://docs.example.com/assets/bg.png":        true,
		"https://docs.example.com/root/images/body.png": true,
	}

	if len(links) != len(want) {
		t.Fatalf("unexpected link count: got %d want %d links=%v", len(links), len(want), links)
	}

	for _, link := range links {
		if !want[link] {
			t.Fatalf("unexpected link extracted: %s", link)
		}
	}
}
