export namespace anim {
	
	export class Effect {
	    t: number;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new Effect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.type = source["type"];
	    }
	}
	export class Expression {
	    t: number;
	    eyes: string;
	    mouth: string;
	
	    static createFrom(source: any = {}) {
	        return new Expression(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.eyes = source["eyes"];
	        this.mouth = source["mouth"];
	    }
	}
	export class Meta {
	    name: string;
	    target: string;
	    description: string;
	    tags: string[];
	    builtin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Meta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.target = source["target"];
	        this.description = source["description"];
	        this.tags = source["tags"];
	        this.builtin = source["builtin"];
	    }
	}
	export class Value {
	    N: number;
	    Vec: number[];
	
	    static createFrom(source: any = {}) {
	        return new Value(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.N = source["N"];
	        this.Vec = source["Vec"];
	    }
	}
	export class Track {
	    slot: string;
	    prop: string;
	    times: number[];
	    values: Value[];
	    interp: string;
	
	    static createFrom(source: any = {}) {
	        return new Track(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slot = source["slot"];
	        this.prop = source["prop"];
	        this.times = source["times"];
	        this.values = this.convertValues(source["values"], Value);
	        this.interp = source["interp"];
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
	export class Spec {
	    name: string;
	    description: string;
	    tags: string[];
	    target: string;
	    duration: number;
	    loop: boolean;
	    tracks: Track[];
	    expressions: Expression[];
	    effects: Effect[];
	    builtin?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.tags = source["tags"];
	        this.target = source["target"];
	        this.duration = source["duration"];
	        this.loop = source["loop"];
	        this.tracks = this.convertValues(source["tracks"], Track);
	        this.expressions = this.convertValues(source["expressions"], Expression);
	        this.effects = this.convertValues(source["effects"], Effect);
	        this.builtin = source["builtin"];
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

export namespace brain {
	
	export class AnimReq {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new AnimReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class MemoryOp {
	    op: string;
	    id: number;
	    kind: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryOp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.content = source["content"];
	    }
	}
	export class PetAction {
	    speech: string;
	    mood: string;
	    animation: string;
	    new_animation_request?: AnimReq;
	    memory_ops: MemoryOp[];
	    suggestion: string;
	
	    static createFrom(source: any = {}) {
	        return new PetAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.speech = source["speech"];
	        this.mood = source["mood"];
	        this.animation = source["animation"];
	        this.new_animation_request = this.convertValues(source["new_animation_request"], AnimReq);
	        this.memory_ops = this.convertValues(source["memory_ops"], MemoryOp);
	        this.suggestion = source["suggestion"];
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

export namespace config {
	
	export class ProviderCfg {
	    model: string;
	    baseURL: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderCfg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.baseURL = source["baseURL"];
	    }
	}
	export class AI {
	    enabled: boolean;
	    provider: string;
	    anthropic: ProviderCfg;
	    openai: ProviderCfg;
	    maxCallsPerHour: number;
	
	    static createFrom(source: any = {}) {
	        return new AI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.provider = source["provider"];
	        this.anthropic = this.convertValues(source["anthropic"], ProviderCfg);
	        this.openai = this.convertValues(source["openai"], ProviderCfg);
	        this.maxCallsPerHour = source["maxCallsPerHour"];
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
	export class General {
	    autostart: boolean;
	    fps: number;
	    monitor: number;
	    respectFullscreen: boolean;
	    debug: boolean;
	
	    static createFrom(source: any = {}) {
	        return new General(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autostart = source["autostart"];
	        this.fps = source["fps"];
	        this.monitor = source["monitor"];
	        this.respectFullscreen = source["respectFullscreen"];
	        this.debug = source["debug"];
	    }
	}
	export class Privacy {
	    watchActivity: boolean;
	    screenshots: boolean;
	    screenshotIntervalMin: number;
	    blocklist: string[];
	    retentionDays: number;
	    excludeFromCapture: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Privacy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.watchActivity = source["watchActivity"];
	        this.screenshots = source["screenshots"];
	        this.screenshotIntervalMin = source["screenshotIntervalMin"];
	        this.blocklist = source["blocklist"];
	        this.retentionDays = source["retentionDays"];
	        this.excludeFromCapture = source["excludeFromCapture"];
	    }
	}
	export class Movement {
	    mode: string;
	    speed: number;
	    activity: number;
	    anchorX: number;
	    anchorY: number;
	
	    static createFrom(source: any = {}) {
	        return new Movement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.speed = source["speed"];
	        this.activity = source["activity"];
	        this.anchorX = source["anchorX"];
	        this.anchorY = source["anchorY"];
	    }
	}
	export class Pet {
	    character: string;
	    name: string;
	    color: string;
	    personality: string;
	    language: string;
	    scale: number;
	
	    static createFrom(source: any = {}) {
	        return new Pet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.character = source["character"];
	        this.name = source["name"];
	        this.color = source["color"];
	        this.personality = source["personality"];
	        this.language = source["language"];
	        this.scale = source["scale"];
	    }
	}
	export class Config {
	    version: number;
	    pet: Pet;
	    movement: Movement;
	    ai: AI;
	    privacy: Privacy;
	    general: General;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.pet = this.convertValues(source["pet"], Pet);
	        this.movement = this.convertValues(source["movement"], Movement);
	        this.ai = this.convertValues(source["ai"], AI);
	        this.privacy = this.convertValues(source["privacy"], Privacy);
	        this.general = this.convertValues(source["general"], General);
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

export namespace main {
	
	export class Bootstrap {
	    config: config.Config;
	    monitor: overlay.Monitor;
	    animations: anim.Spec[];
	    keys: Record<string, string>;
	    fast: boolean;
	    version: string;
	    initError: string;
	    monitors: number;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], config.Config);
	        this.monitor = this.convertValues(source["monitor"], overlay.Monitor);
	        this.animations = this.convertValues(source["animations"], anim.Spec);
	        this.keys = source["keys"];
	        this.fast = source["fast"];
	        this.version = source["version"];
	        this.initError = source["initError"];
	        this.monitors = source["monitors"];
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
	export class PetState {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	    character: string;
	    mode: string;
	    state: string;
	    animation: string;
	    visible: boolean;
	    viewW: number;
	    viewH: number;
	    scrollY: number;
	    dpr: number;
	
	    static createFrom(source: any = {}) {
	        return new PetState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	        this.character = source["character"];
	        this.mode = source["mode"];
	        this.state = source["state"];
	        this.animation = source["animation"];
	        this.visible = source["visible"];
	        this.viewW = source["viewW"];
	        this.viewH = source["viewH"];
	        this.scrollY = source["scrollY"];
	        this.dpr = source["dpr"];
	    }
	}
	export class TestResult {
	    ok: boolean;
	    models: string[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.models = source["models"];
	        this.error = source["error"];
	    }
	}

}

export namespace overlay {
	
	export class Rect {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new Rect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}
	export class Phys {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new Phys(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}
	export class Monitor {
	    index: number;
	    bounds: Phys;
	    work: Phys;
	    scale: number;
	    workCss: Rect;
	    sizeCss: Rect;
	
	    static createFrom(source: any = {}) {
	        return new Monitor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.bounds = this.convertValues(source["bounds"], Phys);
	        this.work = this.convertValues(source["work"], Phys);
	        this.scale = source["scale"];
	        this.workCss = this.convertValues(source["workCss"], Rect);
	        this.sizeCss = this.convertValues(source["sizeCss"], Rect);
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

export namespace store {
	
	export class AppUsage {
	    app: string;
	    minutes: number;
	
	    static createFrom(source: any = {}) {
	        return new AppUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.minutes = source["minutes"];
	    }
	}
	export class Memory {
	    id: number;
	    kind: string;
	    content: string;
	    source: string;
	    confidence: number;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Memory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.content = source["content"];
	        this.source = source["source"];
	        this.confidence = source["confidence"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Stats {
	    days: number;
	    topApps: AppUsage[];
	    todayTopApps: AppUsage[];
	    hourHistogram: number[];
	    typicalStart: string;
	    activeDays: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.days = source["days"];
	        this.topApps = this.convertValues(source["topApps"], AppUsage);
	        this.todayTopApps = this.convertValues(source["todayTopApps"], AppUsage);
	        this.hourHistogram = source["hourHistogram"];
	        this.typicalStart = source["typicalStart"];
	        this.activeDays = source["activeDays"];
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

