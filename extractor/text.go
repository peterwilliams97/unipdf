/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/contentstream"
	"github.com/unidoc/unipdf/v3/core"
	"github.com/unidoc/unipdf/v3/internal/textencoding"
	"github.com/unidoc/unipdf/v3/internal/transform"
	"github.com/unidoc/unipdf/v3/model"
	"golang.org/x/xerrors"
)

// maxFormStack is the maximum form stack recursion depth. It has to be low enough to avoid a stack
// overflow and high enough to accomodate customers' PDFs
const maxFormStack = 20

// ExtractText processes and extracts all text data in content streams and returns as a string.
// It takes into account character encodings in the PDF file, which are decoded by
// CharcodeBytesToUnicode.
// Characters that can't be decoded are replaced with MissingCodeRune ('\ufffd' = �).
func (e *Extractor) ExtractText() (string, error) {
	text, _, _, err := e.ExtractTextWithStats()
	return text, err
}

// ExtractTextWithStats works like ExtractText but returns the number of characters in the output
// (`numChars`) and the number of characters that were not decoded (`numMisses`).
func (e *Extractor) ExtractTextWithStats() (extracted string, numChars int, numMisses int, err error) {
	pageText, numChars, numMisses, err := e.ExtractPageText()
	if err != nil {
		return "", numChars, numMisses, err
	}
	return pageText.Text(), numChars, numMisses, nil
}

// ExtractPageText returns the text contents of `e` (an Extractor for a page) as a PageText.
// TODO(peterwilliams97): The stats complicate this function signature and aren't very useful.
//                        Replace with a function like Extract() (*PageText, error)
func (e *Extractor) ExtractPageText() (*PageText, int, int, error) {
	pt, numChars, numMisses, err := e.extractPageText(e.contents, e.resources, transform.IdentityMatrix(), 0)
	if err != nil {
		return nil, 0, 0, err
	}
	pt.computeViews()
	err = procBuf(pt)
	if err != nil {
		return nil, 0, 0, err
	}

	return pt, numChars, numMisses, nil
}

// extractPageText returns the text contents of content stream `e` and resouces `resources` as a
// PageText.
// This can be called on a page or a form XObject.
func (e *Extractor) extractPageText(contents string, resources *model.PdfPageResources,
	parentCTM transform.Matrix, level int) (
	*PageText, int, int, error) {
	common.Log.Trace("extractPageText: level=%d", level)
	pageText := &PageText{pageSize: e.mediaBox}
	state := newTextState(e.mediaBox)
	var savedStates stateStack
	to := newTextObject(e, resources, contentstream.GraphicsState{}, &state, &savedStates)
	ss := shapesState{parentCTM: parentCTM}
	var inTextObj bool

	if level > maxFormStack {
		err := errors.New("form stack overflow")
		common.Log.Debug("ERROR: extractPageText. recursion level=%d err=%v", level, err)
		return pageText, state.numChars, state.numMisses, err
	}

	// Uncomment the following 3 statements to log the content stream.
	// common.Log.Info("contents* %d -----------------------------", len(contents))
	// fmt.Println(contents)
	// common.Log.Info("contents+ -----------------------------")

	cstreamParser := contentstream.NewContentStreamParser(contents)
	operations, err := cstreamParser.Parse()
	if err != nil {
		common.Log.Debug("ERROR: extractPageText parse failed. err=%v", err)
		return pageText, state.numChars, state.numMisses, err
	}

	processor := contentstream.NewContentStreamProcessor(*operations)

	processor.AddHandler(contentstream.HandlerConditionEnumAllOperands, "",
		func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState,
			resources *model.PdfPageResources) error {
			operand := op.Operand

			if verboseGeom {
				common.Log.Info("&&& op=%s", op)
			}

			switch operand {
			case "q": // Push current graphics state to the stack.
				savedStates.push(&state)
			case "Q": // Pop graphics state from the stack.
				if !savedStates.empty() {
					state = *savedStates.top()
					if len(savedStates) >= 2 {
						savedStates.pop()
					}
				}
			case "BT": // Begin text
				// Begin a text object, initializing the text matrix, Tm, and
				// the text line matrix, Tlm, to the identity matrix. Text
				// objects shall not be nested. A second BT shall not appear
				// before an ET. However, if that happens, all existing marks
				// are added to the  page marks, in order to avoid losing content.
				if inTextObj {
					common.Log.Debug("BT called while in a text object")
					pageText.marks = append(pageText.marks, to.marks...)
				}
				inTextObj = true

				graphicsState := gs
				graphicsState.CTM = parentCTM.Mult(graphicsState.CTM)
				to = newTextObject(e, resources, graphicsState, &state, &savedStates)
			case "ET": // End Text
				// End text object, discarding text matrix. If the current
				// text object contains text marks, they are added to the
				// page text marks collection.
				// The ET operator should always have a matching BT operator.
				// However, if ET appears outside of a text object, the behavior
				// does not change: the text matrices are discarded and all
				// existing marks in the text object are added to the page marks.
				if !inTextObj {
					common.Log.Debug("ET called outside of a text object")
				}
				inTextObj = false
				pageText.marks = append(pageText.marks, to.marks...)
				to.reset()
			case "T*": // Move to start of next text line
				to.nextLine()
			case "Td": // Move text location
				if ok, err := to.checkOp(op, 2, true); !ok {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				x, y, err := toFloatXY(op.Params)
				if err != nil {
					return err
				}
				to.moveText(x, y)
			case "TD": // Move text location and set leading.
				if ok, err := to.checkOp(op, 2, true); !ok {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				x, y, err := toFloatXY(op.Params)
				if err != nil {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				to.moveTextSetLeading(x, y)
			case "Tj": // Show text.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: Tj op=%s err=%v", op, err)
					return err
				}
				charcodes, ok := core.GetStringBytes(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tj op=%s GetStringBytes failed", op)
					return core.ErrTypeError
				}
				return to.showText(charcodes)
			case "TJ": // Show text with adjustable spacing.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: TJ err=%v", err)
					return err
				}
				args, ok := core.GetArray(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: TJ op=%s GetArrayVal failed", op)
					return err
				}
				return to.showTextAdjusted(args)
			case "'": // Move to next line and show text.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: ' err=%v", err)
					return err
				}
				charcodes, ok := core.GetStringBytes(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: ' op=%s GetStringBytes failed", op)
					return core.ErrTypeError
				}
				to.nextLine()
				return to.showText(charcodes)
			case `"`: // Set word and character spacing, move to next line, and show text.
				if ok, err := to.checkOp(op, 3, true); !ok {
					common.Log.Debug("ERROR: \" err=%v", err)
					return err
				}
				x, y, err := toFloatXY(op.Params[:2])
				if err != nil {
					return err
				}
				charcodes, ok := core.GetStringBytes(op.Params[2])
				if !ok {
					common.Log.Debug("ERROR: \" op=%s GetStringBytes failed", op)
					return core.ErrTypeError
				}
				to.setCharSpacing(x)
				to.setWordSpacing(y)
				to.nextLine()
				return to.showText(charcodes)
			case "TL": // Set text leading.
				y, err := floatParam(op)
				if err != nil {
					common.Log.Debug("ERROR: TL err=%v", err)
					return err
				}
				to.setTextLeading(y)
			case "Tc": // Set character spacing.
				y, err := floatParam(op)
				if err != nil {
					common.Log.Debug("ERROR: Tc err=%v", err)
					return err
				}
				to.setCharSpacing(y)
			case "Tf": // Set font.
				if ok, err := to.checkOp(op, 2, true); !ok {
					common.Log.Debug("ERROR: Tf err=%v", err)
					return err
				}
				name, ok := core.GetNameVal(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetNameVal failed", op)
					return core.ErrTypeError
				}
				size, err := core.GetNumberAsFloat(op.Params[1])
				if !ok {
					common.Log.Debug("ERROR: Tf op=%s GetFloatVal failed. err=%v", op, err)
					return err
				}
				err = to.setFont(name, size)
				to.invalidFont = xerrors.Is(err, core.ErrNotSupported)
				if err != nil && !to.invalidFont {
					return err
				}
			case "Tm": // Set text matrix.
				if ok, err := to.checkOp(op, 6, true); !ok {
					common.Log.Debug("ERROR: Tm err=%v", err)
					return err
				}
				floats, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				to.setTextMatrix(floats)
			case "Tr": // Set text rendering mode.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: Tr err=%v", err)
					return err
				}
				mode, ok := core.GetIntVal(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: Tr op=%s GetIntVal failed", op)
					return core.ErrTypeError
				}
				to.setTextRenderMode(mode)
			case "Ts": // Set text rise.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: Ts err=%v", err)
					return err
				}
				y, err := core.GetNumberAsFloat(op.Params[0])
				if err != nil {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				to.setTextRise(y)
			case "Tw": // Set word spacing.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				y, err := core.GetNumberAsFloat(op.Params[0])
				if err != nil {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				to.setWordSpacing(y)
			case "Tz": // Set horizontal scaling.
				if ok, err := to.checkOp(op, 1, true); !ok {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				y, err := core.GetNumberAsFloat(op.Params[0])
				if err != nil {
					common.Log.Debug("ERROR: err=%v", err)
					return err
				}
				to.setHorizScaling(y)

			//
			// Path operators.
			//

			case "cm": // Update CTM
				ss.ctm = gs.CTM
			case "m": // Move to.
				if len(op.Params) != 2 {
					common.Log.Debug("WARN: error while processing `m` operator: %v. Output may be incorrect.",
						model.ErrRange)
					return nil
				}
				xy, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					return err
				}
				common.Log.Debug("Move to: %.2f", xy)
				ss.moveTo(xy[0], xy[1])
			case "l": // Line to.
				if len(op.Params) != 2 {
					common.Log.Debug("WARN: error while processing `l` operator: %v. Output may be incorrect.",
						model.ErrRange)
					return nil
				}
				xy, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					return err
				}
				ss.lineTo(xy[0], xy[1])
			case "c": // Cubic bezier.
				if len(op.Params) != 6 {
					return model.ErrRange
				}
				cbp, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					return err
				}
				common.Log.Debug("Cubic bezier params: %.2f", cbp)
				ss.cubicTo(cbp[0], cbp[1], cbp[2], cbp[3], cbp[4], cbp[5])
			case "v", "y": // Cubic bezier.
				if len(op.Params) != 4 {
					return model.ErrRange
				}
				cbp, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					return err
				}
				common.Log.Debug("Cubic bezier params: %.2f", cbp)
				ss.quadraticTo(cbp[0], cbp[1], cbp[2], cbp[3])
			case "h": // Close current subpath.
				ss.closePath()
			case "re": // Rectangle.
				if len(op.Params) != 4 {
					return model.ErrRange
				}
				xywh, err := core.GetNumbersAsFloat(op.Params)
				if err != nil {
					panic(err)
					return err
				}
				ss.drawRectangle(xywh[0], xywh[1], xywh[2], xywh[3])
				ss.closePath()
			case "S": // Stroke
				ss.stroke(&pageText.strokes)
				ss.clearPath()
			case "s": // Close and stroke.
				ss.closePath()
				ss.stroke(&pageText.strokes)
				ss.clearPath()
			case "F": // Fill
				ss.fill(&pageText.fills)
				ss.clearPath()
			case "f": // Close and fill.
				ss.closePath()
				ss.fill(&pageText.fills)
				ss.clearPath()
			case "B", "B*": // Fill then stroke the path. "B" non-zero winding rule. "B*" odd-even
				ss.fill(&pageText.fills)
				ss.stroke(&pageText.strokes)
				ss.clearPath()
			case "b", "b*": //  Close, fill and stroke the path  "b" non-zero winding rule. "b*" odd-even
				ss.closePath()
				ss.fill(&pageText.fills)
				ss.stroke(&pageText.strokes)
				ss.clearPath()
			case "n": // End the current path without filling or stroking.
				ss.clearPath()

			case "Do": // Handle XObjects by recursing through form XObjects.
				if len(op.Params) == 0 {
					common.Log.Debug("ERROR: expected XObject name operand for Do operator. Got %+v.", op.Params)
					return core.ErrRangeError
				}

				// Get XObject name.
				name, ok := core.GetName(op.Params[0])
				if !ok {
					common.Log.Debug("ERROR: invalid Do operator XObject name operand: %+v.", op.Params[0])
					return core.ErrTypeError
				}

				_, xtype := resources.GetXObjectByName(*name)
				if xtype != model.XObjectTypeForm {
					break
				}
				// Only process each form once.
				formResult, ok := e.formResults[name.String()]
				if !ok {
					xform, err := resources.GetXObjectFormByName(*name)
					if err != nil {
						common.Log.Debug("ERROR: %v", err)
						return err
					}
					formContent, err := xform.GetContentStream()
					if err != nil {
						common.Log.Debug("ERROR: %v", err)
						return err
					}
					formResources := xform.Resources
					if formResources == nil {
						formResources = resources
					}

					tList, numChars, numMisses, err := e.extractPageText(string(formContent),
						formResources, parentCTM.Mult(gs.CTM), level+1)
					if err != nil {
						common.Log.Debug("ERROR: %v", err)
						return err
					}
					formResult = textResult{*tList, numChars, numMisses}
					e.formResults[name.String()] = formResult
				}

				pageText.marks = append(pageText.marks, formResult.pageText.marks...)
				state.numChars += formResult.numChars
				state.numMisses += formResult.numMisses
			case "rg", "g", "k", "cs", "sc", "scn":
				// Set non-stroking color/colorspace.
				to.gs.ColorspaceNonStroking = gs.ColorspaceNonStroking
				to.gs.ColorNonStroking = gs.ColorNonStroking
			case "RG", "G", "K", "CS", "SC", "SCN":
				// Set stroking color/colorspace.
				to.gs.ColorspaceStroking = gs.ColorspaceStroking
				to.gs.ColorStroking = gs.ColorStroking
			}
			return nil
		})

	err = processor.Process(resources)
	if err != nil {
		common.Log.Debug("ERROR: Processing: err=%v", err)
	}

	strokeRulings := makeStrokeGrids(pageText.strokes)
	fillRulings := makeFillGrids(pageText.fills)

	if verboseShape {
		if len(strokeRulings) > 0 {
			common.Log.Info("Strokes: %d", len(pageText.strokes))
			common.Log.Info("Stroke Grids: %d", len(strokeRulings))
			for i, g := range strokeRulings {
				fmt.Printf("%4d: %d rulings\n", i, len(g))
				for i, v := range g {
					fmt.Printf("%8d: %s\n", i, v)
				}
			}
		}
		if len(fillRulings) > 0 {
			common.Log.Info("Fills: %d", len(pageText.fills))
			common.Log.Info("Fill Grids: %d", len(fillRulings))
			for i, g := range fillRulings {
				fmt.Printf("%4d: %d rulings\n", i, len(g))
				for i, v := range g {
					fmt.Printf("%8d: %s\n", i, v)
				}
			}
		}
	}

	return pageText, state.numChars, state.numMisses, err
}

// textResult is used for holding results of PDF form processig
type textResult struct {
	pageText  PageText
	numChars  int
	numMisses int
}

//
// Text operators
//

// moveText "Td" Moves start of text by `tx`,`ty`.
// Move to the start of the next line, offset from the start of the current line by (tx, ty).
// tx and ty are in unscaled text space units.
func (to *textObject) moveText(tx, ty float64) {
	to.moveLP(tx, ty)
}

// moveTextSetLeading "TD" Move text location and set leading.
// Move to the start of the next line, offset from the start of the current line by (tx, ty). As a
// side effect, this operator shall set the leading parameter in the text state. This operator shall
// have the same effect as this code:
//  −ty TL
//  tx ty Td
func (to *textObject) moveTextSetLeading(tx, ty float64) {
	to.state.tl = -ty
	to.moveLP(tx, ty)
}

// nextLine "T*"" Moves start of text line to next text line
// Move to the start of the next line. This operator has the same effect as the code
//    0 -Tl Td
// where Tl denotes the current leading parameter in the text state. The negative of Tl is used
// here because Tl is the text leading expressed as a positive number. Going to the next line
// entails decreasing the y coordinate. (page 250)
func (to *textObject) nextLine() {
	to.moveLP(0, -to.state.tl)
}

// setTextMatrix "Tm".
// Set the text matrix, Tm, and the text line matrix, Tlm to the Matrix specified by the 6 numbers
// in `f` (page 250).
func (to *textObject) setTextMatrix(f []float64) {
	if len(f) != 6 {
		common.Log.Debug("ERROR: len(f) != 6 (%d)", len(f))
		return
	}
	a, b, c, d, tx, ty := f[0], f[1], f[2], f[3], f[4], f[5]
	to.tm = transform.NewMatrix(a, b, c, d, tx, ty)
	to.tlm = to.tm
}

// showText "Tj". Show a text string.
func (to *textObject) showText(charcodes []byte) error {
	return to.renderText(charcodes)
}

// showTextAdjusted "TJ". Show text with adjustable spacing.
func (to *textObject) showTextAdjusted(args *core.PdfObjectArray) error {
	vertical := false
	for _, o := range args.Elements() {
		switch o.(type) {
		case *core.PdfObjectFloat, *core.PdfObjectInteger:
			x, err := core.GetNumberAsFloat(o)
			if err != nil {
				common.Log.Debug("ERROR: showTextAdjusted. Bad numerical arg. o=%s args=%+v", o, args)
				return err
			}
			dx, dy := -x*0.001*to.state.tfs, 0.0
			if vertical {
				dy, dx = dx, dy
			}
			td := translationMatrix(transform.Point{X: dx, Y: dy})
			to.tm.Concat(td)
		case *core.PdfObjectString:
			charcodes, ok := core.GetStringBytes(o)
			if !ok {
				common.Log.Trace("showTextAdjusted: Bad string arg. o=%s args=%+v", o, args)
				return core.ErrTypeError
			}
			to.renderText(charcodes)
		default:
			common.Log.Debug("ERROR: showTextAdjusted. Unexpected type (%T) args=%+v", o, args)
			return core.ErrTypeError
		}
	}
	return nil
}

// setTextLeading "TL". Set text leading.
func (to *textObject) setTextLeading(y float64) {
	if to == nil {
		return
	}
	to.state.tl = y
}

// setCharSpacing "Tc". Set character spacing.
func (to *textObject) setCharSpacing(x float64) {
	if to == nil {
		return
	}
	to.state.tc = x
	if verboseText {
		common.Log.Info("setCharSpacing: %.2f state=%s", x, to.state.String())
	}
}

// setFont "Tf". Set font.
func (to *textObject) setFont(name string, size float64) error {
	if to == nil {
		return nil
	}
	to.state.tfs = size
	font, err := to.getFont(name)
	if err != nil {
		return err
	}
	to.state.tfont = font
	if to.savedStates.empty() {
		to.savedStates.push(to.state)
	} else {
		to.savedStates.top().tfont = to.state.tfont
	}

	return nil
}

// setTextRenderMode "Tr". Set text rendering mode.
func (to *textObject) setTextRenderMode(mode int) {
	if to == nil {
		return
	}
	to.state.tmode = RenderMode(mode)
}

// setTextRise "Ts". Set text rise.
func (to *textObject) setTextRise(y float64) {
	if to == nil {
		return
	}
	to.state.trise = y
}

// setWordSpacing "Tw". Set word spacing.
func (to *textObject) setWordSpacing(y float64) {
	if to == nil {
		return
	}
	to.state.tw = y
}

// setHorizScaling "Tz". Set horizontal scaling.
func (to *textObject) setHorizScaling(y float64) {
	if to == nil {
		return
	}
	to.state.th = y
}

// floatParam returns the single float parameter of operator `op`, or an error if it doesn't have
// a single float parameter or we aren't in a text stream.
func floatParam(op *contentstream.ContentStreamOperation) (float64, error) {
	if len(op.Params) != 1 {
		err := errors.New("incorrect parameter count")
		common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v",
			op.Operand, 1, len(op.Params), op.Params)
		return 0.0, err
	}
	return core.GetNumberAsFloat(op.Params[0])
}

// checkOp returns true if we are in a text stream and `op` has `numParams` params.
// If `hard` is true and the number of params don't match, an error is returned.
func (to *textObject) checkOp(op *contentstream.ContentStreamOperation, numParams int, hard bool) (
	ok bool, err error) {
	if to == nil {
		var params []core.PdfObject
		if numParams > 0 {
			params = op.Params
			if len(params) > numParams {
				params = params[:numParams]
			}
		}
		common.Log.Debug("%#q operand outside text. params=%+v", op.Operand, params)
	}
	if numParams >= 0 {
		if len(op.Params) != numParams {
			if hard {
				err = errors.New("incorrect parameter count")
			}
			common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v",
				op.Operand, numParams, len(op.Params), op.Params)
			return false, err
		}
	}
	return true, nil
}

// stateStack is the PDF textState stack implementation.
type stateStack []*textState

// String returns a string describing the current state of the textState stack.
func (savedStates *stateStack) String() string {
	parts := []string{fmt.Sprintf("---- font stack: %d", len(*savedStates))}
	for i, state := range *savedStates {
		s := "<nil>"
		if state != nil {
			s = state.String()
		}
		parts = append(parts, fmt.Sprintf("\t%2d: %s", i, s))
	}
	return strings.Join(parts, "\n")
}

// push pushes a copy of `state` onto the textState stack.
func (savedStates *stateStack) push(state *textState) {
	s := *state
	*savedStates = append(*savedStates, &s)
}

// pop pops and returns a copy of the last state on the textState stack there is one or nil if
// there isn't.
func (savedStates *stateStack) pop() *textState {
	if savedStates.empty() {
		return nil
	}
	state := *(*savedStates)[len(*savedStates)-1]
	*savedStates = (*savedStates)[:len(*savedStates)-1]
	return &state
}

// top returns the last saved state if there is one or nil if there isn't.
// NOTE: The return is a pointer. Modifying it will modify the stack.
func (savedStates *stateStack) top() *textState {
	if savedStates.empty() {
		return nil
	}
	return (*savedStates)[savedStates.size()-1]
}

// empty returns true if the textState stack is empty.
func (savedStates *stateStack) empty() bool {
	return len(*savedStates) == 0
}

// size returns the number of elements in the textState stack.
func (savedStates *stateStack) size() int {
	return len(*savedStates)
}

// 9.3 Text State Parameters and Operators (page 243)
// Some of these parameters are expressed in unscaled text space units. This means that they shall
// be specified in a coordinate system that shall be defined by the text matrix, Tm but shall not be
// scaled by the font size parameter, Tfs.

// textState represents the text state.
type textState struct {
	tc       float64        // Character spacing. Unscaled text space units.
	tw       float64        // Word spacing. Unscaled text space units.
	th       float64        // Horizontal scaling.
	tl       float64        // Leading. Unscaled text space units. Used by TD,T*,'," see Table 108.
	tfs      float64        // Text font size.
	tmode    RenderMode     // Text rendering mode.
	trise    float64        // Text rise. Unscaled text space units. Set by Ts.
	tfont    *model.PdfFont // Text font.
	mediaBox model.PdfRectangle
	// For debugging
	numChars  int
	numMisses int
}

// String returns a description of `state`.
func (state *textState) String() string {
	fontName := "[NOT SET]"
	if state.tfont != nil {
		fontName = state.tfont.BaseFont()
	}
	return fmt.Sprintf("tc=%.2f tw=%.2f tfs=%.2f font=%q",
		state.tc, state.tw, state.tfs, fontName)
}

// 9.4.1 General (page 248)
// A PDF text object consists of operators that may show text strings, move the text position, and
// set text state and certain other parameters. In addition, two parameters may be specified only
// within a text object and shall not persist from one text object to the next:
//   • Tm, the text matrix
//   • Tlm, the text line matrix
//
// Text space is converted to device space by this transform (page 252)
// Trm is the text rendering matrix
//        | Tfs x Th   0      0 |
// Trm  = | 0         Tfs     0 | × Tm × CTM
//        | 0         Trise   1 |
// This corresponds to the following code in renderText()
//  trm := to.gs.CTM.Mult(stateMatrix).Mult(to.tm)

// textObject represents a PDF text object.
type textObject struct {
	e           *Extractor
	resources   *model.PdfPageResources
	gs          contentstream.GraphicsState
	state       *textState
	savedStates *stateStack
	tm          transform.Matrix // Text matrix. For the character pointer.
	tlm         transform.Matrix // Text line matrix. For the start of line pointer.
	marks       []*textMark      // Text marks get written here.
	invalidFont bool             // Flag that gets set true when we can't handle the current font.
}

// newTextState returns a default textState.
func newTextState(mediaBox model.PdfRectangle) textState {
	return textState{
		th:       100,
		tmode:    RenderModeFill,
		mediaBox: mediaBox,
	}
}

// newTextObject returns a default textObject.
func newTextObject(e *Extractor, resources *model.PdfPageResources, gs contentstream.GraphicsState,
	state *textState, savedStates *stateStack) *textObject {
	return &textObject{
		e:           e,
		resources:   resources,
		gs:          gs,
		savedStates: savedStates,
		state:       state,
		tm:          transform.IdentityMatrix(),
		tlm:         transform.IdentityMatrix(),
	}
}

// reset sets the text matrix `Tm` and the text line matrix `Tlm` of the text
// object to the identity matrix. In addition, the marks collection is cleared.
func (to *textObject) reset() {
	to.tm = transform.IdentityMatrix()
	to.tlm = transform.IdentityMatrix()
	to.marks = nil
}

// getFillColor returns the fill color of the text object.
func (to *textObject) getFillColor() color.Color {
	return pdfColorToGoColor(to.gs.ColorspaceNonStroking, to.gs.ColorNonStroking)
}

// getStrokeColor returns the stroke color of the text object.
func (to *textObject) getStrokeColor() color.Color {
	return pdfColorToGoColor(to.gs.ColorspaceStroking, to.gs.ColorStroking)
}

// renderText processes and renders byte array `data` for extraction purposes.
// It extracts textMarks based the charcodes in `data` and the currect text and graphics states
// are tracked in `to`.
func (to *textObject) renderText(data []byte) error {
	if to.invalidFont {
		common.Log.Debug("renderText: Invalid font. Not processing.")
		return nil
	}
	font := to.getCurrentFont()
	charcodes := font.BytesToCharcodes(data)
	texts, numChars, numMisses := font.CharcodesToStrings(charcodes)
	if numMisses > 0 {
		common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
	}

	to.state.numChars += numChars
	to.state.numMisses += numMisses

	state := to.state
	tfs := state.tfs
	th := state.th / 100.0
	spaceMetrics, ok := font.GetRuneMetrics(' ')
	if !ok {
		spaceMetrics, ok = font.GetCharMetrics(32)
	}
	if !ok {
		spaceMetrics, _ = model.DefaultFont().GetRuneMetrics(' ')
	}
	spaceWidth := spaceMetrics.Wx * glyphTextRatio
	common.Log.Trace("spaceWidth=%.2f text=%q font=%s fontSize=%.2f", spaceWidth, texts, font, tfs)

	stateMatrix := transform.NewMatrix(
		tfs*th, 0,
		0, tfs,
		0, state.trise)
	if verboseText {
		common.Log.Info("renderText: %d codes=%+v texts=%q", len(charcodes), charcodes, texts)
	}

	common.Log.Trace("renderText: %d codes=%+v runes=%q", len(charcodes), charcodes, len(texts))

	fillColor := to.getFillColor()
	strokeColor := to.getStrokeColor()

	for i, text := range texts {
		r := []rune(text)
		if len(r) == 1 && r[0] == '\x00' {
			continue
		}

		code := charcodes[i]
		// The location of the text on the page in device coordinates is given by trm, the text
		// rendering matrix.
		trm := to.gs.CTM.Mult(to.tm).Mult(stateMatrix)

		// calculate the text location displacement due to writing `r`. We will use this to update
		// to.tm

		// w is the unscaled movement at the end of a word.
		w := 0.0
		if len(r) == 1 && r[0] == 32 {
			w = state.tw
		}

		m, ok := font.GetCharMetrics(code)
		if !ok {
			common.Log.Debug("ERROR: No metric for code=%d r=0x%04x=%+q %s", code, r, r, font)
			return fmt.Errorf("no char metrics: font=%s code=%d", font.String(), code)
		}

		// c is the character size in unscaled text units.
		c := transform.Point{X: m.Wx * glyphTextRatio, Y: m.Wy * glyphTextRatio}

		// t0 is the end of this character.
		// t is the displacement of the text cursor when the character is rendered.
		t0 := transform.Point{X: (c.X*tfs + w) * th}
		t := transform.Point{X: (c.X*tfs + state.tc + w) * th}
		if verboseText {
			common.Log.Info("tfs=%.2f tc=%.2f tw=%.2f th=%.2f", tfs, state.tc, state.tw, th)
			common.Log.Info("dx,dy=%.3f t0=%.3f t=%.3f", c, t0, t)
		}

		// td, td0 are t, t0 in matrix form.
		// td0 is where this character ends. td is where the next character starts.
		td0 := translationMatrix(t0)
		td := translationMatrix(t)
		end := to.gs.CTM.Mult(to.tm).Mult(td0)

		if verboseText {
			common.Log.Info("end:\n\tCTM=%s\n\t tm=%s\n"+
				"\t td=%s xlat=%s\n"+
				"\ttd0=%s\n\t  → %s xlat=%s",
				to.gs.CTM, to.tm,
				td, translation(to.gs.CTM.Mult(to.tm).Mult(td)),
				td0, end, translation(end))
		}

		mark, onPage := to.newTextMark(
			textencoding.ExpandLigatures(r),
			trm,
			translation(end),
			math.Abs(spaceWidth*trm.ScalingFactorX()),
			font,
			to.state.tc,
			fillColor,
			strokeColor)

		if !onPage {
			common.Log.Debug("Text mark outside page. Skipping")
			continue
		}
		if font == nil {
			common.Log.Debug("ERROR: No font.")
		} else if font.Encoder() == nil {
			common.Log.Debug("ERROR: No encoding. font=%s", font)
		} else {
			// TODO: This lookup seems confusing. Went from bytes <-> charcodes already.
			// NOTE: This is needed to register runes by the font encoder - for subsetting (optimization).
			if original, ok := font.Encoder().CharcodeToRune(code); ok {
				mark.original = string(original)
			}
		}
		common.Log.Trace("i=%d code=%d mark=%s trm=%s", i, code, mark, trm)
		to.marks = append(to.marks, &mark)

		// update the text matrix by the displacement of the text location.
		to.tm.Concat(td)
	}

	return nil
}

// glyphTextRatio converts Glyph metrics units to unscaled text space units.
const glyphTextRatio = 1.0 / 1000.0

// translation returns the translation part of `m`.
func translation(m transform.Matrix) transform.Point {
	tx, ty := m.Translation()
	return transform.Point{X: tx, Y: ty}
}

// translationMatrix returns a matrix that translates by `p`.
func translationMatrix(p transform.Point) transform.Matrix {
	return transform.TranslationMatrix(p.X, p.Y)
}

// moveLP moves the start of line pointer by `tx`,`ty` and sets the text pointer to the
// start of line pointer.
// Move to the start of the next line, offset from the start of the current line by (tx, ty).
// `tx` and `ty` are in unscaled text space units.
func (to *textObject) moveLP(tx, ty float64) {
	to.tlm.Concat(transform.NewMatrix(1, 0, 0, 1, tx, ty))
	to.tm = to.tlm
}

// PageText represents the layout of text on a device page.
type PageText struct {
	marks      []*textMark        // Texts and their positions on a PDF page.
	viewText   string             // Extracted page text.
	viewMarks  []TextMark         // Public view of text marks.
	viewTables []TextTable        // Public view of text tables.
	pageSize   model.PdfRectangle // Page size. Used to calculate depth.
	strokes    []*subpath
	fills      []*subpath
}

// String returns a string describing `pt`.
func (pt PageText) String() string {
	summary := fmt.Sprintf("PageText: %d elements", len(pt.marks))
	parts := []string{"-" + summary}
	for _, tm := range pt.marks {
		parts = append(parts, tm.String())
	}
	parts = append(parts, "+"+summary)
	return strings.Join(parts, "\n")
}

// Text returns the extracted page text.
func (pt PageText) Text() string {
	return pt.viewText
}

// ToText returns the page text as a single string.
// Deprecated: This function is deprecated and will be removed in a future major version. Please use
// Text() instead.
func (pt PageText) ToText() string {
	return pt.Text()
}

// Marks returns the TextMark collection for a page. It represents all the text on the page.
func (pt PageText) Marks() *TextMarkArray {
	return &TextMarkArray{marks: pt.viewMarks}
}

// Tables returns the tables extracted from the page.
func (pt PageText) Tables() []TextTable {
	return pt.viewTables
}

// computeViews processes the page TextMarks sorting by position and populates `pt.viewText` and
// `pt.viewMarks` which represent the text and marks in the order which it is read on the page.
// The comments above the TextMark definition describe how to use the []TextMark to
// maps substrings of the page text to locations on the PDF page.
func (pt *PageText) computeViews() {
	// Extract text paragraphs one orientation at a time.
	// If there are texts with several orientations on a page then the all the text of the same
	// orientation gets extracted togther.
	var paras paraList
	n := len(pt.marks)
	for orient := 0; orient < 360 && n > 0; orient += 90 {
		marks := make([]*textMark, 0, len(pt.marks)-n)
		for _, tm := range pt.marks {
			if tm.orient == orient {
				marks = append(marks, tm)
			}
		}
		if len(marks) > 0 {
			parasOrient := makeTextPage(marks, pt.pageSize)
			paras = append(paras, parasOrient...)
			n -= len(marks)
		}
	}
	// Build the public viewable fields from the paraList.
	b := new(bytes.Buffer)
	paras.writeText(b)
	pt.viewText = b.String()
	pt.viewMarks = paras.toTextMarks()
	pt.viewTables = paras.tables()
}

// ApplyArea processes the page text only within the specified area `bbox`.
// Each time ApplyArea is called, it updates the result set in `pt`.
// Can be called multiple times in a row with different bounding boxes.
func (pt *PageText) ApplyArea(bbox model.PdfRectangle) {
	// Extract text paragraphs one orientation at a time.
	// If there are texts with several orientations on a page then the all the text of the same
	// orientation gets extracted togther.

	filtered := make([]*textMark, 0, len(pt.marks))
	for _, mark := range pt.marks {
		if intersects(mark.bbox(), bbox) {
			filtered = append(filtered, mark)
		}
	}

	var paras paraList
	n := len(filtered)
	for orient := 0; orient < 360 && n > 0; orient += 90 {
		marks := make([]*textMark, 0, len(filtered)-n)
		for _, tm := range filtered {
			if tm.orient == orient {
				marks = append(marks, tm)
			}
		}
		if len(marks) > 0 {
			parasOrient := makeTextPage(marks, pt.pageSize)
			paras = append(paras, parasOrient...)
			n -= len(marks)
		}
	}
	// Build the public viewable fields from the paraLis
	b := new(bytes.Buffer)
	paras.writeText(b)
	pt.viewText = b.String()
	pt.viewMarks = paras.toTextMarks()
	pt.viewTables = paras.tables()
}

// TextMarkArray is a collection of TextMarks.
type TextMarkArray struct {
	marks []TextMark
}

// Append appends `mark` to the mark array.
func (ma *TextMarkArray) Append(mark TextMark) {
	ma.marks = append(ma.marks, mark)
}

// String returns a string describing `ma`.
func (ma TextMarkArray) String() string {
	n := len(ma.marks)
	if n == 0 {
		return "EMPTY"
	}
	m0 := ma.marks[0]
	m1 := ma.marks[n-1]
	return fmt.Sprintf("{TEXTMARKARRAY: %d elements\n\tfirst=%s\n\t last=%s}", n, m0, m1)
}

// Elements returns the TextMarks in `ma`.
func (ma *TextMarkArray) Elements() []TextMark {
	return ma.marks
}

// Len returns the number of TextMarks in `ma`.
func (ma *TextMarkArray) Len() int {
	if ma == nil {
		return 0
	}
	return len(ma.marks)
}

// RangeOffset returns the TextMarks in `ma` that overlap text[start:end] in the extracted text.
// These are tm: `start` <= tm.Offset + len(tm.Text) && tm.Offset < `end` where
// `start` and `end` are offsets in the extracted text.
// NOTE: TextMarks can contain multiple characters. e.g. "ffi" for the ﬃ ligature so the first and
// last elements of the returned TextMarkArray may only partially overlap text[start:end].
func (ma *TextMarkArray) RangeOffset(start, end int) (*TextMarkArray, error) {
	if ma == nil {
		return nil, errors.New("ma==nil")
	}
	if end < start {
		return nil, fmt.Errorf("end < start. RangeOffset not defined. start=%d end=%d ", start, end)
	}
	n := len(ma.marks)
	if n == 0 {
		return ma, nil
	}
	if start < ma.marks[0].Offset {
		start = ma.marks[0].Offset
	}
	if end > ma.marks[n-1].Offset+1 {
		end = ma.marks[n-1].Offset + 1
	}

	iStart := sort.Search(n, func(i int) bool { return ma.marks[i].Offset+len(ma.marks[i].Text)-1 >= start })
	if !(0 <= iStart && iStart < n) {
		err := fmt.Errorf("Out of range. start=%d iStart=%d len=%d\n\tfirst=%v\n\t last=%v",
			start, iStart, n, ma.marks[0], ma.marks[n-1])
		return nil, err
	}
	iEnd := sort.Search(n, func(i int) bool { return ma.marks[i].Offset > end-1 })
	if !(0 <= iEnd && iEnd < n) {
		err := fmt.Errorf("Out of range. end=%d iEnd=%d len=%d\n\tfirst=%v\n\t last=%v",
			end, iEnd, n, ma.marks[0], ma.marks[n-1])
		return nil, err
	}
	if iEnd <= iStart {
		// This should never happen.
		return nil, fmt.Errorf("iEnd <= iStart: start=%d end=%d iStart=%d iEnd=%d",
			start, end, iStart, iEnd)
	}
	return &TextMarkArray{marks: ma.marks[iStart:iEnd]}, nil
}

// BBox returns the smallest axis-aligned rectangle that encloses all the TextMarks in `ma`.
func (ma *TextMarkArray) BBox() (model.PdfRectangle, bool) {
	var bbox model.PdfRectangle
	found := false
	for _, tm := range ma.marks {
		if tm.Meta || isTextSpace(tm.Text) {
			continue
		}
		if found {
			bbox = rectUnion(bbox, tm.BBox)
		} else {
			bbox = tm.BBox
			found = true
		}
	}
	return bbox, found
}

// TextMark represents extracted text on a page with information regarding both textual content,
// formatting (font and size) and positioning.
// It is the smallest unit of text on a PDF page, typically a single character.
//
// getBBox() in test_text.go shows how to compute bounding boxes of substrings of extracted text.
// The following code extracts the text on PDF page `page` into `text` then finds the bounding box
// `bbox` of substring `term` in `text`.
//
//     ex, _ := New(page)
//     // handle errors
//     pageText, _, _, err := ex.ExtractPageText()
//     // handle errors
//     text := pageText.Text()
//     textMarks := pageText.Marks()
//
//     	start := strings.Index(text, term)
//      end := start + len(term)
//      spanMarks, err := textMarks.RangeOffset(start, end)
//      // handle errors
//      bbox, ok := spanMarks.BBox()
//      // handle errors
type TextMark struct {
	// Text is the extracted text.
	Text string
	// Original is the text in the PDF. It has not been decoded like `Text`.
	Original string
	// BBox is the bounding box of the text.
	BBox model.PdfRectangle
	// Font is the font the text was drawn with.
	Font *model.PdfFont
	// FontSize is the font size the text was drawn with.
	FontSize float64
	// Offset is the offset of the start of TextMark.Text in the extracted text. If you do this
	//   text, textMarks := pageText.Text(), pageText.Marks()
	//   marks := textMarks.Elements()
	// then marks[i].Offset is the offset of marks[i].Text in text.
	Offset int
	// Meta is set true for spaces and line breaks that we insert in the extracted text. We insert
	// spaces (line breaks) when we see characters that are over a threshold horizontal (vertical)
	//  distance  apart. See wordJoiner (lineJoiner) in PageText.computeViews().
	Meta bool
	// FillColor is the fill color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	FillColor color.Color
	// StrokeColor is the stroke color of the text.
	// The color is nil for spaces and line breaks (i.e. the Meta field is true).
	StrokeColor color.Color
}

// String returns a string describing `tm`.
func (tm TextMark) String() string {
	b := tm.BBox
	var font string
	if tm.Font != nil {
		font = tm.Font.String()
		if len(font) > 50 {
			font = font[:50] + "..."
		}
	}
	var meta string
	if tm.Meta {
		meta = " *M*"
	}
	return fmt.Sprintf("{TextMark: %d %q=%02x (%6.2f, %6.2f) (%6.2f, %6.2f) %s%s}",
		tm.Offset, tm.Text, []rune(tm.Text), b.Llx, b.Lly, b.Urx, b.Ury, font, meta)
}

// spaceMark is a special TextMark used for spaces.
var spaceMark = TextMark{
	Text:        "[X]",
	Original:    " ",
	Meta:        true,
	FillColor:   color.White,
	StrokeColor: color.White,
}

// TextTable represents a table.
// Cells are ordered top-to-bottom, left-to-right.
// Cells[y] is the (0-offset) y'th row in the table.
// Cells[y][x] is the (0-offset) x'th column in the table.
type TextTable struct {
	W, H  int
	Cells [][]TableCell
}

// TableCell is a cell in a TextTable.
type TableCell struct {
	// Text is the extracted text.
	Text string
	// Marks returns the TextMarks corresponding to the text in Text.
	Marks TextMarkArray
}

// getCurrentFont returns the font on top of the font stack, or DefaultFont if the font stack is
// empty.
func (to *textObject) getCurrentFont() *model.PdfFont {
	var font *model.PdfFont
	if !to.savedStates.empty() {
		font = to.savedStates.top().tfont
	}
	if font == nil {
		common.Log.Debug("ERROR: No font defined. Using default.")
		return model.DefaultFont()
	}
	return font
}

// getFont returns the font named `name` if it exists in the page's resources or an error if it
// doesn't. It caches the returned fonts.
func (to *textObject) getFont(name string) (*model.PdfFont, error) {
	if to.e.fontCache != nil {
		to.e.accessCount++
		entry, ok := to.e.fontCache[name]
		if ok {
			entry.access = to.e.accessCount
			return entry.font, nil
		}
	}

	// Font not in cache. Load it.
	font, err := to.getFontDirect(name)
	if err != nil {
		return nil, err
	}

	if to.e.fontCache != nil {
		entry := fontEntry{font, to.e.accessCount}

		// Eject a victim if the cache is full.
		if len(to.e.fontCache) >= maxFontCache {
			var names []string
			for name := range to.e.fontCache {
				names = append(names, name)
			}
			sort.Slice(names, func(i, j int) bool {
				return to.e.fontCache[names[i]].access < to.e.fontCache[names[j]].access
			})
			delete(to.e.fontCache, names[0])
		}
		to.e.fontCache[name] = entry
	}

	return font, nil
}

// fontEntry is a entry in the font cache.
type fontEntry struct {
	font   *model.PdfFont // The font being cached.
	access int64          // Last access. Used to determine LRU cache victims.
}

// maxFontCache is the maximum number of PdfFont's in fontCache.
const maxFontCache = 10

// getFontDirect returns the font named `name` if it exists in the page's resources or an error if
// it doesn't. Accesses page resources directly (not cached).
func (to *textObject) getFontDirect(name string) (*model.PdfFont, error) {
	fontObj, err := to.getFontDict(name)
	if err != nil {
		return nil, err
	}
	font, err := model.NewPdfFontFromPdfObject(fontObj)
	if err != nil {
		common.Log.Debug("getFontDirect: NewPdfFontFromPdfObject failed. name=%#q err=%v", name, err)
	}
	return font, err
}

// getFontDict returns the font dict with key `name` if it exists in the page's or form's Font
// resources or an error if it doesn't.
func (to *textObject) getFontDict(name string) (fontObj core.PdfObject, err error) {
	resources := to.resources
	if resources == nil {
		common.Log.Debug("getFontDict. No resources. name=%#q", name)
		return nil, nil
	}
	fontObj, found := resources.GetFontByName(core.PdfObjectName(name))
	if !found {
		common.Log.Debug("ERROR: getFontDict: Font not found: name=%#q", name)
		return nil, errors.New("font not in resources")
	}
	return fontObj, nil
}

type shapesState struct {
	ctm          transform.Matrix
	parentCTM    transform.Matrix
	subpaths     []*subpath
	freshSubpath bool
	firstPoint   transform.Point // First point of path in device coordinates
}

func (ss *shapesState) String() string {
	return fmt.Sprintf("%d subpaths fresh=%t", len(ss.subpaths), ss.freshSubpath)
}

// moveTo starts a new subpath within the current path starting at the specified point.
// `x` and `y` are in user coordinates.
func (ss *shapesState) moveTo(x, y float64) {
	ss.freshSubpath = true
	ss.firstPoint = ss.devicePoint(x, y)
	if verboseShape {
		common.Log.Info("moveTo(%.2f,%.2f current=%.2f", x, y, ss.firstPoint)
	}
}

// lineTo adds a line segment to the current path starting at the current point.
// `x` and `y` are in user coordinates.
func (ss *shapesState) lineTo(x, y float64) {
	subpath := ss.establishSubpath()
	p := ss.devicePoint(x, y)
	if verboseShape {
		common.Log.Info("lineTo(%.2f,%.2f p=%.2f subpath=%s", x, y, p, subpath)
	}
	subpath.add(p)
}

// cubicTo adds a cubic bezier curve to the current path starting at the current point.
// We only care about straight lines so we just update the current point.
func (ss *shapesState) cubicTo(x1, y1, x2, y2, x3, y3 float64) {
	subpath := ss.establishSubpath()
	subpath.add(ss.devicePoint(x3, y3))
}

// quadraticTo adds a quadratic bezier curve to the current path starting at the current point.
// We only care about straight lines so we just update the current point.
func (ss *shapesState) quadraticTo(x1, y1, x2, y2 float64) {
	subpath := ss.establishSubpath()
	subpath.add(ss.devicePoint(x2, y2))
}

// drawRectangle draws a rectangle of size w,h at position x,y.
func (ss *shapesState) drawRectangle(x, y, w, h float64) {
	if verboseShape {
		ll := ss.devicePoint(x, y)
		ur := ss.devicePoint(x+w, y+h)
		r := model.PdfRectangle{Llx: ll.X, Lly: ll.Y, Urx: ur.X, Ury: ur.Y}
		common.Log.Info("drawRectangle: %6.2f", r)
	}
	ss.newSubPath()
	ss.moveTo(x, y)
	ss.lineTo(x+w, y)
	ss.lineTo(x+w, y+h)
	ss.lineTo(x, y+h)
	ss.closePath()
}

// newSubPath starts a new subpath within the current path.
func (ss *shapesState) newSubPath() {
	ss.clearPath()
	if verboseShape {
		common.Log.Info("newSubPath: %s", ss)
	}
}

// closePath adds a line segment from the current point to the beginning of the current subpath.
// If there is no current point, this is a no-op.
func (ss *shapesState) closePath() {
	if ss.freshSubpath {
		ss.subpaths = append(ss.subpaths, newSubpath(ss.firstPoint))
		ss.freshSubpath = false
	}
	ss.subpaths[len(ss.subpaths)-1].close()
	if verboseShape {
		common.Log.Info("closePath: %s", ss)
	}
}

// clearPath clears the current path. There is no current point after this operation.
func (ss *shapesState) clearPath() {
	ss.subpaths = nil
	ss.freshSubpath = false
	if verboseShape {
		common.Log.Info("CLEAR: ss=%s", ss)
	}
}

// stroke appends the current subpath to `strokes`.
func (ss *shapesState) stroke(strokes *[]*subpath) {
	*strokes = append(*strokes, ss.subpaths...)
	if verboseShape {
		common.Log.Info("STROKE: %d strokes ss=%s", len(*strokes), ss)
	}
}

// fill appends the current subpaths to `fills`.
func (ss *shapesState) fill(fills *[]*subpath) {
	*fills = append(*fills, ss.subpaths...)
	if verboseShape {
		common.Log.Info("FILL: %d fills (%d new) ss=%s", len(*fills), len(ss.subpaths), ss)
		// for i, p := range *fills {
		// 	fmt.Printf("%4d: %s\n", i, p)
		// 	if i == 10 {
		// 		break
		// 	}
		// }
	}
}

// devicePoint returns user coordinates `x`, `y` as a transform.Point in device coordinates.
func (ss *shapesState) devicePoint(x, y float64) transform.Point {
	ctm := ss.parentCTM.Mult(ss.ctm)
	x, y = ctm.Transform(x, y)
	return transform.NewPoint(x, y)
}

// establishSubpath creates a new subpath if one hasn't already been established.
// It reaturns the current subpath.
func (ss *shapesState) establishSubpath() *subpath {
	if lastPoint, established := ss.lastPoint(); !established {
		ss.subpaths = append(ss.subpaths, newSubpath(lastPoint))
	}
	ss.freshSubpath = false
	return ss.subpaths[len(ss.subpaths)-1]
}

// lastPoint returns the last point added to if there was one.
// If there is a fresh point, return it.
// Otherwise if the last subpath was closed, return its last point.
// It neither of the above cases is true we must be in an established subpath.
func (ss *shapesState) lastPoint() (transform.Point, bool) {
	if ss.freshSubpath {
		return ss.firstPoint, false
	}
	n := len(ss.subpaths)
	if n > 0 && ss.subpaths[n-1].closed {
		return ss.subpaths[n-1].last(), false
	}
	return transform.Point{}, true
}

// subpath is a list of points
type subpath struct {
	points []transform.Point // Path points in device coordinates.
	closed bool              // Done with subpath?
}

// newSubpath returns a subpath containing `p`.
func newSubpath(p transform.Point) *subpath {
	return &subpath{points: []transform.Point{p}}
}

// last return the last point in `path`. Caller must check that `path` has at least one element
// before calling.
func (path *subpath) last() transform.Point {
	return path.points[len(path.points)-1]
}

// add adds `points` to `path`.
func (path *subpath) add(points ...transform.Point) {
	path.points = append(path.points, points...)
}

func (path *subpath) clear() {
	*path = subpath{}
}

func (path *subpath) close() {
	if !equalPoints(path.points[0], path.last()) {
		path.add(path.points[0])
	}
	path.closed = true
	path.removeDuplicates()
}

func (path *subpath) removeDuplicates() {
	if len(path.points) == 0 {
		return
	}
	uniques := []transform.Point{path.points[0]}
	for _, p := range path.points[1:] {
		if !equalPoints(p, uniques[len(uniques)-1]) {
			uniques = append(uniques, p)
		}
	}
	path.points = uniques
}

func (path *subpath) String() string {
	p := path.points
	n := len(p)
	if n <= 5 {
		return fmt.Sprintf("%d: %6.2f", n, p)
	}
	return fmt.Sprintf("%d: %6.2f %6.2f ... %6.2f", n, p[0], p[1], p[n-1])
}
