package hclencoder

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"reflect"
	"sort"
)

// tokenize converts a primitive type into tokens. structs and maps are converted into objects and slices are converted
// into tuples.
func tokenize(in reflect.Value, meta fieldMeta) (tkns hclwrite.Tokens, err error) {

	tokenEqual := hclwrite.Token{
		Type:         hclsyntax.TokenEqual,
		Bytes:        []byte("="),
		SpacesBefore: 0,
	}
	tokenComma := hclwrite.Token{
		Type:         hclsyntax.TokenComma,
		Bytes:        []byte(","),
		SpacesBefore: 0,
	}
	tokenOCurlyBrace := hclwrite.Token{
		Type:         hclsyntax.TokenOBrace,
		Bytes:        []byte("{"),
		SpacesBefore: 0,
	}
	tokenCCurlyBrace := hclwrite.Token{
		Type:         hclsyntax.TokenCBrace,
		Bytes:        []byte("}"),
		SpacesBefore: 0,
	}

	switch in.Kind() {
	case reflect.Bool:
		return hclwrite.TokensForValue(cty.BoolVal(in.Bool())), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return hclwrite.TokensForValue(cty.NumberUIntVal(in.Uint())), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return hclwrite.TokensForValue(cty.NumberIntVal(in.Int())), nil

	case reflect.Float64:
		return hclwrite.TokensForValue(cty.NumberFloatVal(in.Float())), nil
	case reflect.String:
		val := in.String()
		if !meta.expression {
			return hclwrite.TokensForValue(cty.StringVal(val)), nil
		}
		// Unfortunately hcl escapes template expressions (${...}) when using hclwrite.TokensForValue. So we escape
		// everything but template expressions and then parse the expression into tokens.
		tokens, diags := hclsyntax.LexExpression([]byte(val), meta.name, hcl.Pos{
			Line:   0,
			Column: 0,
			Byte:   0,
		})

		if diags != nil {
			return nil, fmt.Errorf("error when parsing string %s: %v", val, diags.Error())
		}
		return convertTokens(tokens), nil
	case reflect.Pointer, reflect.Interface:
		val, isNil := deref(in)
		if isNil {
			return nil, nil
		}
		return tokenize(val, meta)
	case reflect.Struct:
		var tokens []*hclwrite.Token
		tokens = append(tokens, &tokenOCurlyBrace)
		for i := 0; i < in.NumField(); i++ {
			field := in.Type().Field(i)
			meta := extractFieldMeta(field)

			rawVal := in.Field(i)
			if meta.omitEmpty {
				zeroVal := reflect.Zero(rawVal.Type()).Interface()
				if reflect.DeepEqual(rawVal.Interface(), zeroVal) {
					continue
				}
			}
			val, err := tokenize(rawVal, meta)
			if err != nil {
				return nil, err
			}
			for _, tkn := range hclwrite.TokensForValue(cty.StringVal(meta.name)) {
				tokens = append(tokens, tkn)
			}
			tokens = append(tokens, &tokenEqual)
			for _, tkn := range val {
				tokens = append(tokens, tkn)
			}
			if i < in.NumField()-1 {
				tokens = append(tokens, &tokenComma)
			}
		}
		tokens = append(tokens, &tokenCCurlyBrace)
		return tokens, nil
	case reflect.Slice:
		var tokens []*hclwrite.Token
		tokens = append(tokens, &hclwrite.Token{
			Type:         hclsyntax.TokenOBrace,
			Bytes:        []byte("["),
			SpacesBefore: 0,
		})
		for i := 0; i < in.Len(); i++ {
			value, err := tokenize(in.Index(i), meta)
			if err != nil {
				return nil, err
			}
			for _, tkn := range value {
				tokens = append(tokens, tkn)
			}
			if i < in.Len()-1 {
				tokens = append(tokens, &tokenComma)
			}
		}
		tokens = append(tokens, &hclwrite.Token{
			Type:         hclsyntax.TokenCBrace,
			Bytes:        []byte("]"),
			SpacesBefore: 0,
		})
		return tokens, nil
	case reflect.Map:
		if keyType := in.Type().Key().Kind(); keyType != reflect.String {
			return nil, fmt.Errorf("map keys must be strings, %s given", keyType)
		}
		var tokens []*hclwrite.Token
		tokens = append(tokens, &tokenOCurlyBrace)

		var keys []string
		for _, k := range in.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		for i, k := range keys {
			val, err := tokenize(in.MapIndex(reflect.ValueOf(k)), meta)
			if err != nil {
				return nil, err
			}
			for _, tkn := range hclwrite.TokensForValue(cty.StringVal(k)) {
				tokens = append(tokens, tkn)
			}
			tokens = append(tokens, &tokenEqual)
			for _, tkn := range val {
				tokens = append(tokens, tkn)
			}
			if i < len(keys)-1 {
				tokens = append(tokens, &tokenComma)
			}
		}
		tokens = append(tokens, &tokenCCurlyBrace)
		return tokens, nil
	}

	return nil, fmt.Errorf("cannot encode primitive kind %s to token", in.Kind())
}

func convertTokens(tokens hclsyntax.Tokens) hclwrite.Tokens {
	var result []*hclwrite.Token
	for _, token := range tokens {
		result = append(result, &hclwrite.Token{
			Type:         token.Type,
			Bytes:        token.Bytes,
			SpacesBefore: 0,
		})
	}
	return result
}
