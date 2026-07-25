export namespace main {
	
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
	
	    static createFrom(source: any = {}) {
	        return new TreeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modelName = source["modelName"];
	        this.root = this.convertValues(source["root"], Part);
	        this.selectedId = source["selectedId"];
	        this.canUndo = source["canUndo"];
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

