export namespace cut {
	
	export class Bed {
	    x: number;
	    y: number;
	    z: number;
	
	    static createFrom(source: any = {}) {
	        return new Bed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.z = source["z"];
	    }
	}
	export class PinSpec {
	    enabled: boolean;
	    count: number;
	    diameter: number;
	    length: number;
	    clearance: number;
	    minWall: number;
	    pegOnPart: number;
	    dowel: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PinSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.count = source["count"];
	        this.diameter = source["diameter"];
	        this.length = source["length"];
	        this.clearance = source["clearance"];
	        this.minWall = source["minWall"];
	        this.pegOnPart = source["pegOnPart"];
	        this.dowel = source["dowel"];
	    }
	}
	export class SkippedPin {
	    x: number;
	    y: number;
	    z: number;
	    reason: string;
	    measured: number;
	    required: number;
	
	    static createFrom(source: any = {}) {
	        return new SkippedPin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.z = source["z"];
	        this.reason = source["reason"];
	        this.measured = source["measured"];
	        this.required = source["required"];
	    }
	}

}

export namespace main {
	
	export class RepairView {
	    holesFilled: number;
	    trianglesAdded: number;
	    degenerateRemoved: number;
	    before: string;
	    after: string;
	    closed: boolean;
	    nonManifoldEdges: number;
	    shellsDropped: number;
	    trianglesRemoved: number;
	    bodies: number;
	
	    static createFrom(source: any = {}) {
	        return new RepairView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.holesFilled = source["holesFilled"];
	        this.trianglesAdded = source["trianglesAdded"];
	        this.degenerateRemoved = source["degenerateRemoved"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.closed = source["closed"];
	        this.nonManifoldEdges = source["nonManifoldEdges"];
	        this.shellsDropped = source["shellsDropped"];
	        this.trianglesRemoved = source["trianglesRemoved"];
	        this.bodies = source["bodies"];
	    }
	}
	export class Part {
	    id: string;
	    name: string;
	    tris: number;
	    volume: number;
	    min: number[];
	    size: number[];
	    watertight: boolean;
	    children: Part[];
	
	    static createFrom(source: any = {}) {
	        return new Part(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.tris = source["tris"];
	        this.volume = source["volume"];
	        this.min = source["min"];
	        this.size = source["size"];
	        this.watertight = source["watertight"];
	        this.children = this.convertValues(source["children"], Part);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TreeView {
	    modelName: string;
	    root?: Part;
	    selectedId: string;
	    canUndo: boolean;
	    repair?: RepairView;
	
	    static createFrom(source: any = {}) {
	        return new TreeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modelName = source["modelName"];
	        this.root = this.convertValues(source["root"], Part);
	        this.selectedId = source["selectedId"];
	        this.canUndo = source["canUndo"];
	        this.repair = this.convertValues(source["repair"], RepairView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AutoSplitOutcome {
	    tree?: TreeView;
	    cutsMade: number;
	    stillTooBig: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new AutoSplitOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tree = this.convertValues(source["tree"], TreeView);
	        this.cutsMade = source["cutsMade"];
	        this.stillTooBig = source["stillTooBig"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CutOutcome {
	    tree?: TreeView;
	    watertight: boolean;
	    warnings: string[];
	    pinsPlaced: number;
	    pinsRequested: number;
	    pinsSkipped: cut.SkippedPin[];
	
	    static createFrom(source: any = {}) {
	        return new CutOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tree = this.convertValues(source["tree"], TreeView);
	        this.watertight = source["watertight"];
	        this.warnings = source["warnings"];
	        this.pinsPlaced = source["pinsPlaced"];
	        this.pinsRequested = source["pinsRequested"];
	        this.pinsSkipped = this.convertValues(source["pinsSkipped"], cut.SkippedPin);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportOutcome {
	    dir: string;
	    files: string[];
	    notWatertight: string[];
	    cancelled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExportOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.files = source["files"];
	        this.notWatertight = source["notWatertight"];
	        this.cancelled = source["cancelled"];
	    }
	}
	
	export class PlaneInput {
	    origin: number[];
	    normal: number[];
	    u: number[];
	    v: number[];
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new PlaneInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.origin = source["origin"];
	        this.normal = source["normal"];
	        this.u = source["u"];
	        this.v = source["v"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class PlateReport {
	    name: string;
	    plate: number;
	    overhangArea: number;
	    baseArea: number;
	    height: number;
	    rotated: boolean;
	    fitsPlate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PlateReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.plate = source["plate"];
	        this.overhangArea = source["overhangArea"];
	        this.baseArea = source["baseArea"];
	        this.height = source["height"];
	        this.rotated = source["rotated"];
	        this.fitsPlate = source["fitsPlate"];
	    }
	}
	export class PlateOutcome {
	    cancelled: boolean;
	    path: string;
	    plates: number;
	    oriented: PlateReport[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new PlateOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cancelled = source["cancelled"];
	        this.path = source["path"];
	        this.plates = source["plates"];
	        this.oriented = this.convertValues(source["oriented"], PlateReport);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class SeparateOutcome {
	    tree?: TreeView;
	    bodies: number;
	    flagged: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new SeparateOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tree = this.convertValues(source["tree"], TreeView);
	        this.bodies = source["bodies"];
	        this.flagged = source["flagged"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace stl {
	
	export class Tri {
	    A: number[];
	    B: number[];
	    C: number[];
	
	    static createFrom(source: any = {}) {
	        return new Tri(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.A = source["A"];
	        this.B = source["B"];
	        this.C = source["C"];
	    }
	}
	export class Mesh {
	    Tris: Tri[];
	
	    static createFrom(source: any = {}) {
	        return new Mesh(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Tris = this.convertValues(source["Tris"], Tri);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

