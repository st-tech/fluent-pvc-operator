package utils

import (
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"

	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/davecgh/go-spew/spew"
)

// ComputeHash returns a hash value calculated from object and a collisionCount
// to avoid hash collision. The hash will be safe encoded to avoid bad words.
// ref. https://github.com/kubernetes/kubernetes/blob/f803daaca74ecd2a9b75d8945a6b5403aa5e47a9/pkg/controller/controller_utils.go#L1189-L1201
func ComputeHash(obj interface{}, collisionCount *int32) string {
	objHasher := fnv.New32a()
	DeepHashObject(objHasher, obj)

	// Add collisionCount in the hash if it exists.
	if collisionCount != nil {
		collisionCountBytes := make([]byte, 8)
		binary.LittleEndian.PutUint32(collisionCountBytes, uint32(*collisionCount))
		objHasher.Write(collisionCountBytes)
	}

	return rand.SafeEncodeString(fmt.Sprint(objHasher.Sum32()))
}

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}
	printer.Fprintf(hasher, "%#v", objectToWrite)
}
