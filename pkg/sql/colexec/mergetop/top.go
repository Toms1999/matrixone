// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mergetop

import (
	"bytes"
	"container/heap"
	"fmt"

	"github.com/matrixorigin/matrixone/pkg/compare"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/sql/colexec"
	"github.com/matrixorigin/matrixone/pkg/vm/process"
)

func String(arg any, buf *bytes.Buffer) {
	ap := arg.(*Argument)
	buf.WriteString("mergetop([")
	for i, f := range ap.Fs {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(f.String())
	}
	buf.WriteString(fmt.Sprintf("], %v)", ap.Limit))
}

func Prepare(_ *process.Process, arg any) error {
	ap := arg.(*Argument)
	ap.ctr = new(container)
	ap.ctr.sels = make([]int64, 0, ap.Limit)
	ap.ctr.poses = make([]int32, 0, len(ap.Fs))
	return nil
}

func Call(idx int, proc *process.Process, arg any) (bool, error) {
	anal := proc.GetAnalyze(idx)
	anal.Start()
	defer anal.Stop()
	ap := arg.(*Argument)
	ctr := ap.ctr
	for {
		switch ctr.state {
		case Build:
			if ap.Limit == 0 {
				ctr.state = End
				proc.Reg.InputBatch = nil
				return true, nil
			}
			if err := ctr.build(ap, proc, anal); err != nil {
				ctr.state = End
				return true, err
			}
			ctr.state = Eval
		case Eval:
			ctr.state = End
			if ctr.bat == nil {
				proc.SetInputBatch(nil)
				return true, nil
			}
			return true, ctr.eval(ap.Limit, proc, anal)
		default:
			proc.SetInputBatch(nil)
			return true, nil
		}
	}
}

func (ctr *container) build(ap *Argument, proc *process.Process, anal process.Analyze) error {
	for {
		if len(proc.Reg.MergeReceivers) == 0 {
			break
		}
		for i := 0; i < len(proc.Reg.MergeReceivers); i++ {
			reg := proc.Reg.MergeReceivers[i]
			bat := <-reg.Ch
			if bat == nil {
				proc.Reg.MergeReceivers = append(proc.Reg.MergeReceivers[:i], proc.Reg.MergeReceivers[i+1:]...)
				i--
				continue
			}
			if bat.Length() == 0 {
				i--
				continue
			}
			anal.Input(bat)
			ctr.n = len(bat.Vecs)
			ctr.poses = ctr.poses[:0]
			for _, f := range ap.Fs {
				vec, err := colexec.EvalExpr(bat, proc, f.E)
				if err != nil {
					return err
				}
				flg := true
				for i := range bat.Vecs {
					if bat.Vecs[i] == vec {
						flg = false
						ctr.poses = append(ctr.poses, int32(i))
						break
					}
				}
				if flg {
					ctr.poses = append(ctr.poses, int32(len(bat.Vecs)))
					bat.Vecs = append(bat.Vecs, vec)
				}
			}
			if ctr.bat == nil {
				mp := make(map[int]int)
				for i, pos := range ctr.poses {
					mp[int(pos)] = i
				}
				ctr.bat = batch.NewWithSize(len(bat.Vecs))
				for i, vec := range bat.Vecs {
					ctr.bat.Vecs[i] = vector.New(vec.Typ)
				}
				ctr.cmps = make([]compare.Compare, len(bat.Vecs))
				for i := range ctr.cmps {
					if pos, ok := mp[i]; ok {
						ctr.cmps[i] = compare.New(bat.Vecs[i].Typ, ap.Fs[pos].Type == colexec.Descending)
					} else {
						ctr.cmps[i] = compare.New(bat.Vecs[i].Typ, true)
					}
				}
			}
			if err := ctr.processBatch(ap.Limit, bat, proc); err != nil {
				bat.Clean(proc.Mp)
				return err
			}
			bat.Clean(proc.Mp)
		}
	}
	return nil
}

func (ctr *container) processBatch(limit int64, bat *batch.Batch, proc *process.Process) error {
	var start int64

	length := int64(len(bat.Zs))
	if n := int64(len(ctr.sels)); n < limit {
		start = limit - n
		if start > length {
			start = length
		}
		for i := int64(0); i < start; i++ {
			for j, vec := range ctr.bat.Vecs {
				if err := vector.UnionOne(vec, bat.Vecs[j], i, proc.Mp); err != nil {
					return err
				}
			}
			ctr.sels = append(ctr.sels, n)
			ctr.bat.Zs = append(ctr.bat.Zs, bat.Zs[i])
			n++
		}
		if n == limit {
			ctr.sort()
		}
	}
	if start == length {
		return nil
	}

	// bat is still have items
	for i, cmp := range ctr.cmps {
		cmp.Set(1, bat.Vecs[i])
	}
	for i, j := start, length; i < j; i++ {
		if ctr.compare(1, 0, i, ctr.sels[0]) < 0 {
			for _, cmp := range ctr.cmps {
				if err := cmp.Copy(1, 0, i, ctr.sels[0], proc); err != nil {
					return err
				}
				ctr.bat.Zs[0] = bat.Zs[i]
			}
			heap.Fix(ctr, 0)
		}
	}
	return nil
}

func (ctr *container) eval(limit int64, proc *process.Process, anal process.Analyze) error {
	if int64(len(ctr.sels)) < limit {
		ctr.sort()
	}
	for i, cmp := range ctr.cmps {
		ctr.bat.Vecs[i] = cmp.Vector()
	}
	sels := make([]int64, len(ctr.sels))
	for i, j := 0, len(ctr.sels); i < j; i++ {
		sels[len(sels)-1-i] = heap.Pop(ctr).(int64)
	}
	if err := ctr.bat.Shuffle(sels, proc.Mp); err != nil {
		ctr.bat.Clean(proc.Mp)
		ctr.bat = nil
	}
	for i := ctr.n; i < len(ctr.bat.Vecs); i++ {
		vector.Clean(ctr.bat.Vecs[i], proc.Mp)
	}
	ctr.bat.Vecs = ctr.bat.Vecs[:ctr.n]
	ctr.bat.ExpandNulls()
	anal.Output(ctr.bat)
	proc.SetInputBatch(ctr.bat)
	ctr.bat = nil
	return nil
}

// do sort work for heap, and result order will be set in container.sels
func (ctr *container) sort() {
	for i, cmp := range ctr.cmps {
		cmp.Set(0, ctr.bat.Vecs[i])
	}
	heap.Init(ctr)
}
