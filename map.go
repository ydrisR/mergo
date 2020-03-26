// Copyright 2014 Dario Castañé. All rights reserved.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Based on src/pkg/reflect/deepequal.go from official
// golang's stdlib.

package mergo

import (
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf8"
)

const (
	hashConstant = 17
)

func changeInitialCase(s string, mapper func(rune) rune) string {
	if s == "" {
		return s
	}

	r, n := utf8.DecodeRuneInString(s)

	return string(mapper(r)) + s[n:]
}

func isExported(field reflect.StructField) bool {
	r, _ := utf8.DecodeRuneInString(field.Name)
	return r >= 'A' && r <= 'Z'
}

func mapMap(dst, src reflect.Value, config *Config) {
	overwrite := config.Overwrite
	dstMap := dst.Interface().(map[string]interface{})

	for i, n := 0, src.NumField(); i < n; i++ {
		srcType := src.Type()

		field := srcType.Field(i)
		if !isExported(field) {
			continue
		}

		fieldName := changeInitialCase(field.Name, unicode.ToLower)
		if v, ok := dstMap[fieldName]; !ok || (isEmptyValue(reflect.ValueOf(v)) || overwrite) {
			dstMap[fieldName] = src.Field(i).Interface()
		}
	}
}

func mapStruct(dst, src reflect.Value, visited map[uintptr]*visit, depth int, config *Config) error {
	zeroValue := reflect.Value{}

	srcMap := src.Interface().(map[string]interface{})
	for key := range srcMap {
		config.overwriteWithEmptyValue = true
		srcValue := srcMap[key]
		fieldName := changeInitialCase(key, unicode.ToUpper)
		dstElement := dst.FieldByName(fieldName)

		if dstElement == zeroValue {
			// We discard it because the field doesn't exist.
			continue
		}

		srcElement := reflect.ValueOf(srcValue)
		dstKind := dstElement.Kind()
		srcKind := srcElement.Kind()

		if srcKind == reflect.Ptr && dstKind != reflect.Ptr {
			srcElement = srcElement.Elem()
			srcKind = reflect.TypeOf(srcElement.Interface()).Kind()
		} else if dstKind == reflect.Ptr {
			// Can this work? I guess it can't.
			if srcKind != reflect.Ptr && srcElement.CanAddr() {
				srcPtr := srcElement.Addr()
				srcElement = reflect.ValueOf(srcPtr)
				srcKind = reflect.Ptr
			}
		}

		if !srcElement.IsValid() {
			continue
		}

		depth++

		switch {
		case srcKind == dstKind:
			fallthrough
		case dstKind == reflect.Interface && dstElement.Kind() == reflect.Interface:
			if _, err := deepMerge(dstElement, srcElement, visited, depth, config); err != nil {
				return err
			}
		case srcKind == reflect.Map:
			if err := deepMap(dstElement, srcElement, visited, depth, config); err != nil {
				return err
			}
		default:
			return fmt.Errorf("type mismatch on %s field: found %v, expected %v", fieldName, srcKind, dstKind)
		}
	}

	return nil
}

// Traverses recursively both values, assigning src's fields values to dst.
// The map argument tracks comparisons that have already been seen, which allows
// short circuiting on recursive types.
func deepMap(dst, src reflect.Value, visited map[uintptr]*visit, depth int, config *Config) error {
	if dst.CanAddr() {
		addr := dst.UnsafeAddr()
		h := hashConstant * addr
		seen := visited[h]
		typ := dst.Type()

		for p := seen; p != nil; p = p.next {
			if p.ptr == addr && p.typ == typ {
				return nil
			}
		}
		// Remember, remember...
		visited[h] = &visit{addr, typ, seen}
	}

	switch dst.Kind() {
	case reflect.Map:
		mapMap(dst, src, config)
	case reflect.Ptr:
		if dst.IsNil() {
			v := reflect.New(dst.Type().Elem())
			dst.Set(v)
		}

		dst = dst.Elem()

		fallthrough
	case reflect.Struct:
		return mapStruct(dst, src, visited, depth, config)
	}

	return nil
}

// Map sets fields' values in dst from src.
// src can be a map with string keys or a struct. dst must be the opposite:
// if src is a map, dst must be a valid pointer to struct. If src is a struct,
// dst must be map[string]interface{}.
// It won't merge unexported (private) fields and will do recursively
// any exported field.
// If dst is a map, keys will be src fields' names in lower camel case.
// Missing key in src that doesn't match a field in dst will be skipped. This
// doesn't apply if dst is a map.
// This is separated method from Merge because it is cleaner and it keeps sane
// semantics: merging equal types, mapping different (restricted) types.
func Map(dst, src interface{}, opts ...func(*Config)) error {
	return _map(dst, src, opts...)
}

// MapWithOverwrite will do the same as Map except that non-empty dst attributes will be overridden by
// non-empty src attribute values.
// Deprecated: Use Map(…) with WithOverride
func MapWithOverwrite(dst, src interface{}, opts ...func(*Config)) error {
	return _map(dst, src, append(opts, WithOverride)...)
}

func _map(dst, src interface{}, opts ...func(*Config)) error {
	if dst != nil && reflect.ValueOf(dst).Kind() != reflect.Ptr {
		return ErrNonPointerAgument
	}

	var (
		vDst, vSrc reflect.Value
		err        error
		config     = &Config{}
	)

	for _, opt := range opts {
		opt(config)
	}

	if vDst, vSrc, err = resolveValues(dst, src); err != nil {
		return err
	}
	// To be friction-less, we redirect equal-type arguments
	// to deepMerge. Only because arguments can be anything.
	if vSrc.Kind() == vDst.Kind() {
		_, err := deepMerge(vDst, vSrc, make(map[uintptr]*visit), 0, config)
		return err
	}

	switch vSrc.Kind() {
	case reflect.Struct:
		if vDst.Kind() != reflect.Map {
			return ErrExpectedMapAsDestination
		}
	case reflect.Map:
		if vDst.Kind() != reflect.Struct {
			return ErrExpectedStructAsDestination
		}
	default:
		return ErrNotSupported
	}

	return deepMap(vDst, vSrc, make(map[uintptr]*visit), 0, config)
}
