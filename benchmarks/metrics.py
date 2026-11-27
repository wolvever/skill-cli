"""Metrics distinguish retrieval context coverage from model tool selection."""
import math
import random

def selection(selected,gold):
    selected=set(selected);gold=set(gold);tp=len(selected&gold)
    precision=tp/len(selected) if selected else (1.0 if not gold else 0.0)
    recall=tp/len(gold) if gold else None
    return dict(tp=tp,fp=len(selected-gold),fn=len(gold-selected),precision=precision,recall=recall,exact_match=selected==gold,negative_abstention=(not selected) if not gold else None)

def aggregate(rows):
    if not rows:
        return dict(n=0,precision=None,recall=None,exact_match=None,negative_abstention=None)
    tp=sum(r['tp'] for r in rows);fp=sum(r['fp'] for r in rows);fn=sum(r['fn'] for r in rows)
    negatives=[r['negative_abstention'] for r in rows if r['negative_abstention'] is not None]
    positive_recall=[r['recall'] for r in rows if r['recall'] is not None]
    return dict(n=len(rows),micro_precision=tp/(tp+fp) if tp+fp else None,micro_recall=tp/(tp+fn) if tp+fn else None,macro_precision=sum(r['precision'] for r in rows)/len(rows),macro_positive_recall=sum(positive_recall)/len(positive_recall) if positive_recall else None,exact_match=sum(r['exact_match'] for r in rows)/len(rows),negative_abstention=sum(negatives)/len(negatives) if negatives else None)

def paired_bootstrap(left,right,seed=7,samples=2000):
    """Mean paired difference and percentile 95% interval; tasks are sampling units."""
    if len(left)!=len(right) or not left:
        raise ValueError('paired nonempty observations required')
    diffs=[a-b for a,b in zip(left,right)];rng=random.Random(seed)
    estimates=sorted(sum(rng.choice(diffs) for _ in diffs)/len(diffs) for _ in range(samples))
    return dict(mean=sum(diffs)/len(diffs),ci95=[estimates[math.floor(.025*(samples-1))],estimates[math.ceil(.975*(samples-1))]])
