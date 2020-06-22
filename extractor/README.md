TEXT EXTRACTION CODE
====================

BASIC IDEAS
-----------

There are two [directions](https://www.w3.org/International/questions/qa-scripts.en#directions)s\.

- *reading*
- *depth*

In English text,
- the *reading* direction is left to right, increasing X in the PDF coordinate system.
- the *depth* directon is top to bottom, decreasing Y in the PDF coordinate system.

*depth* is the distance from the bottom of a word's bounding box from the top of the page.
depth := pageSize.Ury - r.Lly

* Pages are divided into rectangular regions called `textPara`s.
* The `textPara`s in a page are sorted in reading order (the order they are read in, not the
*reading* direction above).
* Each `textPara` contains `textLine`s, lines with the `textPara`'s bounding box.
* Each `textLine` has extracted for the line in its `text()` function.
* Page text is extracted by iterating over `textPara`s and within each `textPara` iterating over its
`textLine`s.
* The textMarks corresponding to extracted text can be found.


HOW TEXT IS EXTRACTED
---------------------

`text_page.go` **makeTextPage** is the top level function that builds the `textPara`s.

* A page's `textMark`s are obtained from its contentstream.
* The `textMark`s are grouped into `textWord`s based on their bounding boxes.
* The `textWords`s are grouped into `textParas`s based on their bounding boxes.
* `textPara`s arranged as cells in a table are converted to `textPara`s containing `textTable`s
 which have the cells as elements.
* The textParas in reading order using.
* The words in each `textPara` are arranged into`textLine`s.


### `textWord` discovery

`makeTextWords()` combines `textMark`s into `textWord`s.

TODO
====
Remove serial code????
Reinstate rotated text handling.
Reinstate hyphen diacritic composition.
Reinstate duplicate text removal
Get these files working:
		challenging-modified.pdf
		transitions_test.pdf

### radical.txt
Evaluate the potential impact of each
s t r a t e g y u s i n g t h e V i s i o n /


TEST FILES
---------
bruce.pdf for char spacing save/restore.

challenging-modified.pdf
transitions_test.pdf





TODO
Breuel O(n) before map[from,to]bool
right and bottom for para formation
search for receivers
check if functions are called

check for names
    hyphenated
    spaceAfter
    word0 para1 before etc
