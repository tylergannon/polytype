package main

import (
	"fmt"
	"reflect"

	"example.com/runtime-consumer/codec"
	"example.com/runtime-consumer/model"
	"github.com/tylergannon/devalue/v5"
)

func main() {
	want := model.Packet{Name: "Ada", Values: []int{1, 2}}
	wire, err := codec.StringifyPacket(want)
	if err != nil {
		panic(err)
	}
	if wire != `[{"name":1,"values":2},"Ada",[3,4],1,2]` {
		panic(wire)
	}
	tree, err := devalue.Parse(wire, nil)
	if err != nil {
		panic(err)
	}
	got, err := codec.DecodePacket(tree)
	if err != nil {
		panic(err)
	}
	if !reflect.DeepEqual(got, want) {
		panic(fmt.Sprintf("decoded: %#v", got))
	}
	fmt.Println("standalone-runtime-wire-and-roundtrip=ok")
	fmt.Println("upstream=" + devalue.UpstreamVersion)
	fmt.Println(wire)
}
