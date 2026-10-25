package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
)

// Battle.net's Agent lists every game it installed, wherever the player put it, in product.db: a
// protobuf whose field 1 repeats one record per product (field 2 its product code, field 3 its
// settings, whose field 1 is the install folder). Field numbers are read from the Agent's own file.
func battleNetInstalls(db string) []string {
	data, err := os.ReadFile(db)
	if err != nil {
		return nil
	}
	var roots []string
	for _, product := range fields(data, 1) {
		if code := fields(product, 2); len(code) != 1 || !slices.ContainsFunc(knownClients(), func(c gameClient) bool { return c.Product == string(code[0]) }) {
			continue
		}
		for _, settings := range fields(product, 3) {
			for _, path := range fields(settings, 1) {
				if len(path) > 0 {
					roots = append(roots, filepath.Clean(string(path)))
				}
			}
		}
	}
	return roots
}

// fields returns the length-delimited values of one field number in a protobuf message; a malformed
// message yields what was read before the fault.
func fields(message []byte, number uint64) [][]byte {
	var values [][]byte
	for len(message) > 0 {
		key, n := binary.Uvarint(message)
		if n <= 0 {
			return values
		}
		message = message[n:]
		switch key & 7 {
		case 0:
			if _, n = binary.Uvarint(message); n <= 0 {
				return values
			}
			message = message[n:]
		case 1, 5:
			size := map[uint64]int{1: 8, 5: 4}[key&7]
			if len(message) < size {
				return values
			}
			message = message[size:]
		case 2:
			size, n := binary.Uvarint(message)
			if n <= 0 || uint64(len(message)-n) < size {
				return values
			}
			if key>>3 == number {
				values = append(values, message[n:n+int(size)])
			}
			message = message[n+int(size):]
		default:
			return values
		}
	}
	return values
}
