# Reference documents

word4win should read every document here the way LibreOffice Writer does,
and it should save a document back to its own format without losing
anything it read.

## Sources

`go run ./tools/genrefdocs` (from the repository root, with LibreOffice's
`soffice` on the PATH) writes them:

| Document          | What it covers |
| ----------------- | -------------- |
| `text`            | bold, italic, underline kinds, strikethrough, sub- and superscript, caps, hidden text, fonts, sizes, colours, highlights, shading, Unicode, breaks, symbols |
| `paragraphs`      | alignment, indents (left, right, first line, hanging, negative), spacing, line spacing, tab stops and leaders, borders, shading, page breaks, right to left |
| `lists`           | bullets on three levels, numbers, letters, Roman numerals, multilevel numbering, start at 5, continue and restart, items with more paragraphs |
| `tables`          | header row, cell alignment, widths, merged cells, cell colours, lists and a table in a cell, row height, no borders, a wide table |
| `links-images`    | external, mail, field and bookmark links, PNG, JPEG, GIF and BMP pictures, scaled and centred pictures |
| `styles`          | Title, Subtitle, Heading 1 to 6, Quote, Intense Quote, custom and missing styles, character styles, direct formatting over a style |
| `extras`          | header, footer, footnote, endnote, comment, tracked changes, fields, content control, text box, a landscape section |
| `text-*.txt`      | plain text in ASCII, UTF-8 (with and without BOM, LF and CRLF), UTF-16 LE and BE and Windows-1252 |
| `markdown.md`     | CommonMark and GitHub Markdown: emphasis, links, breaks, nested and task lists, quotes, code, tables, a picture, headings |
| `html.html`       | tags and CSS that word4win reads: fonts, sizes, colours, alignment, indents, lists, tables, embedded and linked pictures, entities |

The seven feature documents are there as `.docx` (written by the
generator), `.odt` and `.rtf` (made by LibreOffice from the DOCX).
`picture.png` is the picture the Markdown and HTML link to.

## Writer references

For every source the generator also lets LibreOffice Writer convert it to
RTF, saved next to it with `.writer.rtf` added to the full name:
`lists.docx` gives `lists.docx.writer.rtf`. That is what Writer made of the
document.

- Text files are read as UTF-8 or by their BOM, the Windows-1252 file with
  that encoding set explicitly (headless Writer would read it as UTF-8).
- HTML is opened in Writer (`HTML (StarWriter)`), not in Writer/Web.
- Markdown has no reference: LibreOffice 24 cannot read Markdown.

The references are made by the installed LibreOffice, so a different
version can give slightly different files, and every run changes the
files LibreOffice makes a little (it writes the time into them), even when
nothing else changed. Regenerate the sources and the references together,
and commit them together.

## Tests

`internal/doc/writer_test.go`, `TestWriterReferences`: for every
`.writer.rtf` it reads the source with word4win and the reference with
word4win's RTF reader, and compares the text and the formatting (both
written as HTML). Where they differ, word4win changes to do what Writer
does. In the reference, list items get the numbers Writer showed (its
`\listtext`): Writer's RTF export does not keep every start number in its
list definitions.

Some differences come from Writer's RTF export or its default styles, not
from what Writer made of the document. The test leaves them out:

- the reference's default font counts as the default font;
- pictures are compared by their pixels (Writer converts GIF and BMP in its
  own way), not by their alternative text (Writer's RTF drops a picture's
  title);
- a header row counts as a bold first row (Writer's RTF drops "repeat as
  header");
- for HTML, the formatting of Writer's default styles: the 115% line
  spacing of "Text Body", the sizes of headings 3 to 6, the colour of
  "Internet Link" and the 1 cm indent of "Quotations".

`writerBugs` in the test lists the lines where Writer is wrong or does not
do what browsers do, and that may differ: it reads the italic of one part of
a link into the other part when it reads its own RTF, and in HTML it
ignores `<del>`, `vertical-align` and a picture's width without a height,
and replaces tabs in `<pre>` by spaces. The HTML source has those in
paragraphs of their own, so only those lines are left out.

`internal/doc/roundtrip_test.go`, `TestRoundTrip`: every source is opened
as the app opens it, into the editor as RTF (`WriteRTF`, then `ReadRTF`; the
RichEdit control in between is only on Windows), and saved back to its own
format: RTF, Markdown, HTML or text. DOCX and ODT cannot be saved, so they
are saved as RTF, as the app does. Reading the saved file must give the same
text and formatting as the opened source. Text files are compared on text
only.

Both tests report the first line that differs. With `WORD4WIN_DUMP` set to
a directory, a failing test also writes both sides there as HTML, to
compare them with a diff tool.
