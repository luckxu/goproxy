// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"sync"
)

const (
	DEFAULT_BUFFER_SIZE = 1312
)

type buffer struct {
	size int
	next *buffer
	data []byte
}

type bufferHeader struct {
	holdcnt int
	cnt     int
	mutex   sync.RWMutex
	pool    *BufferPool
	head    *buffer
	tail    *buffer
}

type BufferPool struct {
	next    *buffer
	mutex   sync.Mutex
	holdcnt uint32
	allcnt  uint32
	usedcnt uint32
}

//推送缓存至尾部
//@clean 满时清除标识，当clean为true且池容器大于设定的保持数时将内存归还至根缓存池
func (bh *bufferHeader) append(b *buffer, clean bool) {
	bh.mutex.Lock()
	defer bh.mutex.Unlock()
	if clean && bh.cnt > bh.holdcnt {
		bh.pool.put(b)
		return
	}
	bh.cnt++
	b.next = nil
	if bh.head == nil {
		bh.tail = b
		bh.head = b
	} else {
		bh.tail.next = b
		bh.tail = b
	}
}

//推送缓存至头部
//@clean 满时清除标识，当clean为true且池容器大于设定的保持数时将内存归还至根缓存池
func (bh *bufferHeader) push(b *buffer, clean bool) {
	bh.mutex.Lock()
	defer bh.mutex.Unlock()
	if clean && bh.cnt > bh.holdcnt {
		bh.pool.put(b)
		return
	}
	bh.cnt++
	b.next = nil
	if bh.head == nil {
		bh.head = b
		bh.tail = b
	} else {
		b.next = bh.head
		bh.head = b
	}
}

//从池头部获取缓存，计数减1
func (bh *bufferHeader) pop() *buffer {
	bh.mutex.Lock()
	defer bh.mutex.Unlock()
	b := bh.head
	if b != nil {
		if b.next == nil {
			bh.head = nil
			bh.tail = nil
		} else {
			bh.head = bh.head.next
		}
		bh.cnt--
	}
	return b
}

//是否空
func (bh *bufferHeader) isEmpty() bool {
	bh.mutex.RLock()
	defer bh.mutex.RUnlock()
	return bh.cnt == 0
}

//渐满，缓存总数大于当前预定保持数量2/3时true
func (bh *bufferHeader) almostFull() bool {
	bh.mutex.RLock()
	defer bh.mutex.RUnlock()
	return bh.cnt > (bh.holdcnt * 2 / 3)
}

//渐空，缓存总数小于当前预定保持数量1/3时true
func (bh *bufferHeader) almostEmpty() bool {
	bh.mutex.RLock()
	defer bh.mutex.RUnlock()
	return bh.cnt < (bh.holdcnt / 3)
}

//创建新的缓存池
//@holecnt 最大保持数。释放缓存时，当缓存数多于保持数时释放内存，否则保留
func NewBufferPool(holdcnt uint32) *BufferPool {
	p := &BufferPool{holdcnt: holdcnt}
	return p
}

//从缓存池获取缓存
func (bp *BufferPool) get() *buffer {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	var b *buffer
	bp.usedcnt++
	if bp.next == nil {
		b = &buffer{}
		b.data = make([]byte, DEFAULT_BUFFER_SIZE)
		bp.allcnt++
	} else {
		b = bp.next
		bp.next = bp.next.next
		b.next = nil
	}
	return b
}

//向缓存池归还缓存，当总数多于最大保持数时释放内存
func (bp *BufferPool) put(b *buffer) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	bp.usedcnt--
	if bp.allcnt > bp.holdcnt {
		bp.allcnt--
	} else {
		b.next = bp.next
		bp.next = b
	}
}

//向缓存池归还整个列表的缓存，既然多于最大保持数时也不释放内存
func (bp *BufferPool) appendList(h *bufferHeader) {
	if h.head == nil {
		return
	}
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	h.tail.next = bp.next
	bp.next = h.head
	h.tail = nil
	h.head = nil
}
