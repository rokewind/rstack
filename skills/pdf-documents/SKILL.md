---
name: pdf-documents
description: "Create, revise and visually check PDF documents by writing print-ready HTML and rendering it with pdf-render. Use when asked to make, update, render or check the layout of a PDF. To review only a PDF's wording, read it directly."
---

# PDF documents

Write the document as one HTML file with print CSS. Render it with `pdf-render`
and look at every page before calling it done. The HTML is the source: edit it and
re-render it. Never edit the PDF itself.

## Before you start

Check the target directory or repository for its own instructions, templates and
naming conventions, such as an `AGENTS.md` or `templates/` folder. Those take
precedence over this skill's defaults.

Put files where the user asks. Treat documents as private unless told otherwise:
don't put them in public repositories. Producing a PDF doesn't authorize sending,
uploading or committing it, so ask before doing any of those. Keep earlier
versions unless the user asks you to replace them.

## Write

- With no local template, start from [the default template](assets/document.html).
  It has an A4 page, a footer with "page / pages", headings, a table and an example
  page break. Set the accent colour, title, meta line and footer text first.
- Keep images and fonts local, next to the HTML, and use relative paths. Remote
  assets make the output depend on the network.
- Use `.page` to start a new page, and `.keep` or `break-inside: avoid` for blocks
  that mustn't split. Don't add spacer paragraphs to push content around.

## Render

```sh
pdf-render --png <image-dir> path/to/document.html        # writes path/to/document.pdf
pdf-render -o out.pdf --json path/to/document.html
```

The tool uses an installed Chrome or Chromium; set `PDF_RENDER_CHROME` to choose
one. `--png` needs `pdftoppm` from poppler. If it's missing, read the PDF directly
if your tools can do that. A failed render leaves any existing PDF unchanged.
If `pdf-render` isn't installed, see [installation](../../tools/README.md).

## Check every page

Look at every page image, not just the first. Fix and re-render until all of these
hold:

- Nothing is clipped, overlapping or running into the footer. Tables and other
  blocks that should stay together aren't split, and no heading sits alone at the
  bottom of a page.
- The page count is what you expect, with no blank or near-empty last page.
- The footer text and page numbers are right on every page.
- Figures, dates and names are consistent everywhere they appear, and any totals
  equal the sum of their parts.
- No placeholder text from the template remains.

Report the PDF path, the page count, and anything you couldn't verify.

## Review an existing PDF

Read the PDF, or render page images with `pdftoppm -r 110 -png file.pdf <dir>/page`.
Comment on its content and layout. Don't recreate it unless asked. If the user
wants it rebuilt, write the HTML and follow the steps above.
