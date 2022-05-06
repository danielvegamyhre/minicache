package ring

import (
	"testing"
	"github.com/malwaredllc/minicache/node"
	. "github.com/smartystreets/goconvey/convey"
)

var (
	node1id = "node1"
	node2id = "node2" 
	node3id = "node3"
)

func TestAddNode(t *testing.T) {
	Convey("Given empty ring", t, func() {
		Convey("Then it should add node", func() {
			r := NewRing()
			r.AddNode(node1id)

			So(r.Nodes.Len(), ShouldEqual, 1)

			Convey("Then node should've hashed id", func() {
				So(r.Nodes[0].HashId, ShouldHaveSameTypeAs, uint32(0))
			})
		})

		Convey("Then it should add node & sort by node id", func() {
			r := NewRing()
			r.AddNode(node1id)
			r.AddNode(node2id)

			So(r.Nodes.Len(), ShouldEqual, 2)

			node1hash := node.HashId(node1id)
			node2hash := node.HashId(node2id)

			So(node1hash, ShouldBeGreaterThan, node2hash)

			So(r.Nodes[0].Id, ShouldEqual, node2id)
			So(r.Nodes[1].Id, ShouldEqual, node1id)
		})
	})
}

func TestRemoveNode(t *testing.T) {
	Convey("Given ring with nodes", t, func() {
		r := NewRing()
		r.AddNode(node1id)
		r.AddNode(node2id)
		r.AddNode(node3id)

		Convey("When node doesn't exist", func() {
			Convey("Then it should return error", func() {
				err := r.RemoveNode("nonexistent")
				So(err, ShouldEqual, ErrNodeNotFound)
			})
		})

		Convey("When node exists", func() {
			Convey("Then it should remove node", func() {
				err := r.RemoveNode(node2id)
				So(err, ShouldBeNil)

				So(r.Nodes.Len(), ShouldEqual, 2)

				So(r.Nodes[0].Id, ShouldEqual, node3id)
				So(r.Nodes[1].Id, ShouldEqual, node1id)
			})
		})
	})
}

func TestGet(t *testing.T) {
	Convey("Given ring with 1 node", t, func() {
		r := NewRing()
		r.AddNode(node1id)

		Convey("Then it should return that node regardless of input", func() {
			insertnode := r.Get("id")
			So(insertnode, ShouldEqual, node1id)

			insertnode = r.Get("anykey")
			So(insertnode, ShouldEqual, node1id)
		})
	})

	Convey("Given ring with multiple nodes", t, func() {
		insertid := "justa"

		r := NewRing()
		r.AddNode(node1id)
		r.AddNode(node2id)
		r.AddNode(node3id)

		Convey("Then it should return node closest", func() {
			node1hash := node.HashId(node1id)
			node3hash := node.HashId(node2id)
			inserthash := node.HashId(insertid)

			So(inserthash, ShouldBeLessThan, node1hash)
			So(inserthash, ShouldBeGreaterThan, node3hash)

			insertnode := r.Get(insertid)
			So(insertnode, ShouldEqual, node1id)
		})
	})
}