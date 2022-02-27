package hclencoder

import (
	"errors"
	"fmt"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"reflect"
	"strings"
)

const (
	// HCLTagName is the struct field tag used by the HCL decoder. The
	// values from this tag are used in the same way as the decoder.
	HCLTagName = "hcl"

	// KeyTag indicates that the value of the field should be part of
	// the parent object block's key, not a property of that block
	KeyTag string = "key"

	// SquashTag is attached to fields of a struct and indicates
	// to the encoder to lift the fields of that value into the parent
	// block's scope transparently.
	SquashTag string = "squash"

	// Blocks is attached to a slice of objects and indicates that
	// the slice should be treated as multiple separate blocks rather than
	// a list.
	Blocks string = "blocks"

	// Expression indicates that this field should not be quoted.
	Expression string = "expr"

	// UnusedKeysTag is a flag that indicates any unused keys found by the
	// decoder are stored in this field of type []string. This has the same
	// behavior as the OmitTag and is not encoded.
	UnusedKeysTag string = "unusedKeys"

	// DecodedFieldsTag is a flag that indicates all fields decoded are
	// stored in this field of type []string. This has the same behavior as
	// the OmitTag and is not encoded.
	DecodedFieldsTag string = "decodedFields"

	// HCLETagName is the struct field tag used by this package. The
	// values from this tag are used in conjunction with HCLTag values.
	HCLETagName = "hcle"

	// OmitTag will omit this field from encoding. This is the similar
	// behavior to `json:"-"`.
	OmitTag string = "omit"

	// OmitEmptyTag will omit this field if it is a zero value. This
	// is similar behavior to `json:",omitempty"`
	OmitEmptyTag string = "omitempty"
)

type fieldMeta struct {
	anonymous     bool
	name          string
	key           bool
	squash        bool
	repeatBlock   bool
	expression    bool
	unusedKeys    bool
	decodedFields bool
	omit          bool
	omitEmpty     bool
}

type node struct {
	Block     *hclwrite.Block
	BlockList []*hclwrite.Block
	Value     *cty.Value
	Tokens    hclwrite.Tokens
}

func (n node) isValue() bool {
	return n.Value != nil
}

func (n node) isBlock() bool {
	return n.Block != nil
}

func (n node) isBlockList() bool {
	return n.BlockList != nil
}

func (n node) isTokens() bool {
	return n.Tokens != nil
}

func encode(in reflect.Value) (node *node, err error) {
	return encodeField(in, fieldMeta{})
}

// encode converts a reflected valued into an HCL ast.node in a depth-first manner.
func encodeField(in reflect.Value, meta fieldMeta) (node *node, err error) {
	in, isNil := deref(in)
	if isNil {
		return nil, nil
	}

	switch in.Kind() {

	case reflect.Bool, reflect.Float64, reflect.String,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return encodePrimitive(in, meta)

	case reflect.Slice:
		return encodeList(in, meta)

	case reflect.Map:
		return encodePrimitive(in, meta)

	case reflect.Struct:
		if in.Type().AssignableTo(reflect.TypeOf(cty.Value{})) {
			meta.expression = true
			str, _ := ValueToString(in.Interface().(cty.Value))
			return encodePrimitive(reflect.ValueOf(str), meta)
		}
		return encodeStruct(in, meta)
	default:
		return nil, fmt.Errorf("cannot encode kind %s to HCL", in.Kind())
	}
}

// encodePrimitive converts a primitive value into a node contains its tokens
func encodePrimitive(in reflect.Value, meta fieldMeta) (*node, error) {
	// Keys must be literals, so we don't tokenize.
	if meta.key {
		k := cty.StringVal(in.String())
		return &node{Value: &k}, nil
	}
	tkn, err := tokenize(in, meta)
	if err != nil {
		return nil, err
	}

	return &node{Tokens: tkn}, nil
}

// encodeList converts a slice into either a block list or a primitive list depending on its element type
func encodeList(in reflect.Value, meta fieldMeta) (*node, error) {
	childType := in.Type().Elem()

childLoop:
	for {
		switch childType.Kind() {
		case reflect.Ptr:
			childType = childType.Elem()
		default:
			break childLoop
		}
	}

	switch childType.Kind() {
	case reflect.Map, reflect.Struct, reflect.Interface:
		return encodeBlockList(in, meta)
	default:
		return encodePrimitiveList(in, meta)
	}
}

// encodePrimitiveList converts a slice of primitive values to an ast.ListType. An
// ast.ObjectKey is never returned.
func encodePrimitiveList(in reflect.Value, meta fieldMeta) (*node, error) {
	return encodePrimitive(in, meta)
}

// encodeBlockList converts a slice of non-primitive types to an ast.ObjectList. An
// ast.ObjectKey is never returned.
func encodeBlockList(in reflect.Value, meta fieldMeta) (*node, error) {
	var blocks []*hclwrite.Block

	if !meta.repeatBlock {
		return encodePrimitiveList(in, meta)
	}

	for i := 0; i < in.Len(); i++ {
		node, err := encodeStruct(in.Index(i), meta)
		if err != nil {
			return nil, err
		}
		if node == nil {
			continue
		}
		blocks = append(blocks, node.Block)
	}

	return &node{BlockList: blocks}, nil
}

// encodeStruct converts a struct type into a block
func encodeStruct(in reflect.Value, parentMeta fieldMeta) (*node, error) {
	l := in.NumField()
	block := hclwrite.NewBlock(parentMeta.name, nil)

	for i := 0; i < l; i++ {
		field := in.Type().Field(i)
		meta := extractFieldMeta(field)

		// these tags are used for debugging the decoder
		// they should not be output
		if meta.unusedKeys || meta.decodedFields || meta.omit {
			continue
		}

		// if the OmitEmptyTag is provided, check if the value is its zero value.
		rawVal := in.Field(i)
		if meta.omitEmpty {
			zeroVal := reflect.Zero(rawVal.Type()).Interface()
			if reflect.DeepEqual(rawVal.Interface(), zeroVal) {
				continue
			}
		}

		val, err := encodeField(rawVal, meta)
		if err != nil {
			return nil, err
		}
		if val == nil {
			continue
		}

		// this field is a key and should be bubbled up to the parent node
		if meta.key {
			if val.isValue() && (*val.Value).Type() == cty.String {
				label := (*val.Value).AsString()
				block.SetLabels(append(block.Labels(), label))
				continue
			}
			return nil, errors.New("struct key fields must be string literals")
		}

		if meta.squash && !val.isBlock() {
			return nil, errors.New("squash fields must be structs")
		}

		if val.isBlock() {
			if meta.squash {
				squashBlock(val.Block, block.Body())
				for _, label := range val.Block.Labels() {
					block.SetLabels(append(block.Labels(), label))
				}
			} else {
				block.Body().AppendBlock(val.Block)
			}
			continue
		} else if val.isBlockList() {
			for _, innerBlock := range val.BlockList {
				block.Body().AppendBlock(innerBlock)
			}
		} else if val.isValue() {
			block.Body().SetAttributeValue(meta.name, *val.Value)
		} else if val.isTokens() {
			block.Body().SetAttributeRaw(meta.name, val.Tokens)
		} else {
			return nil, errors.New("unknown value type")
		}

	}

	return &node{Block: block}, nil
}

func squashBlock(innerBlock *hclwrite.Block, block *hclwrite.Body) {
	tkns := innerBlock.Body().BuildTokens(nil)
	block.AppendUnstructuredTokens(tkns)

}

// extractFieldMeta pulls information about struct fields and the optional HCL tags
func extractFieldMeta(f reflect.StructField) (meta fieldMeta) {
	if f.Anonymous {
		meta.anonymous = true
		meta.name = f.Type.Name()
	} else {
		meta.name = f.Name
	}

	tags := strings.Split(f.Tag.Get(HCLTagName), ",")
	if len(tags) > 0 {
		if tags[0] != "" {
			meta.name = tags[0]
		}

		for _, tag := range tags[1:] {
			switch tag {
			case KeyTag:
				meta.key = true
			case SquashTag:
				meta.squash = true
			case DecodedFieldsTag:
				meta.decodedFields = true
			case UnusedKeysTag:
				meta.unusedKeys = true
			case Blocks:
				meta.repeatBlock = true
			case Expression:
				meta.expression = true
			}
		}
	}

	tags = strings.Split(f.Tag.Get(HCLETagName), ",")
	for _, tag := range tags {
		switch tag {
		case OmitTag:
			meta.omit = true
		case OmitEmptyTag:
			meta.omitEmpty = true
		}
	}

	return
}

// deref safely dereferences interface and pointer values to their underlying value types.
// It also detects if that value is invalid or nil.
func deref(in reflect.Value) (val reflect.Value, isNil bool) {
	switch in.Kind() {
	case reflect.Invalid:
		return in, true
	case reflect.Interface, reflect.Ptr:
		if in.IsNil() {
			return in, true
		}
		// recurse for the elusive double pointer
		return deref(in.Elem())
	case reflect.Slice, reflect.Map:
		return in, in.IsNil()
	default:
		return in, false
	}
}
